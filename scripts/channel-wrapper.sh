#!/bin/bash
# gobrrr-channels: launches Claude Code with channel plugins, monitors, rotates
# Handles: idle timeout, memory ceiling, max uptime, exponential backoff
set -euo pipefail

GOBRRR_DIR="${HOME}/.gobrrr"
CONFIG="${GOBRRR_DIR}/config.json"
CLAUDE_BIN="${HOME}/.local/bin/claude"
ACTIVITY_MARKER="${GOBRRR_DIR}/.last-activity"
RESTART_COUNT_FILE="${GOBRRR_DIR}/.restart-count"
PTY_LOG="${GOBRRR_DIR}/logs/session-pty.log"
CHECK_INTERVAL=60  # seconds between health checks

# --- Load config via jq ---
cfg() { jq -r "$1" "$CONFIG"; }

IDLE_TIMEOUT_MIN=$(cfg '.telegram_session.idle_threshold_min // 30')
MEMORY_CEILING_MB=$(cfg '.telegram_session.memory_ceiling_mb // 3072')
MAX_UPTIME_HOURS=$(cfg '.telegram_session.max_uptime_hours // 6')
MAX_RESTART_ATTEMPTS=$(cfg '.telegram_session.max_restart_attempts // 6')
CHANNELS=$(cfg '[.telegram_session.channels // ["plugin:gobrrr-telegram@gobrrr-local","plugin:gobrrr-relay@gobrrr-local"] | .[] | "--dangerously-load-development-channels", .] | join(" ")')

# Telegram notification config
TELEGRAM_BOT_TOKEN=""
TELEGRAM_CHAT_ID=""
if [ -f "${HOME}/.claude/channels/telegram/.env" ]; then
    TELEGRAM_BOT_TOKEN=$(grep -oP 'TELEGRAM_BOT_TOKEN=\K.*' "${HOME}/.claude/channels/telegram/.env" || true)
fi
if [ -f "${GOBRRR_DIR}/telegram-chat-id" ]; then
    TELEGRAM_CHAT_ID=$(cat "${GOBRRR_DIR}/telegram-chat-id")
fi

# --- Telegram notifications ---
send_telegram() {
    local msg="$1"
    if [ -n "$TELEGRAM_BOT_TOKEN" ] && [ -n "$TELEGRAM_CHAT_ID" ]; then
        curl -s "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
            -d chat_id="${TELEGRAM_CHAT_ID}" \
            -d text="$msg" \
            -d parse_mode="Markdown" >/dev/null 2>&1 || true
    fi
}

# --- Backoff state ---
load_restart_count() {
    if [ -f "$RESTART_COUNT_FILE" ]; then
        cat "$RESTART_COUNT_FILE"
    else
        echo 0
    fi
}

save_restart_count() { echo "$1" > "$RESTART_COUNT_FILE"; }
reset_restart_count() { echo 0 > "$RESTART_COUNT_FILE"; }

get_backoff_sec() {
    local count=$1
    case $count in
        0|1) echo 30 ;;
        2) echo 60 ;;
        3) echo 120 ;;
        4|5) echo 300 ;;
        *) echo -1 ;;
    esac
}

# --- Activity tracking ---
# Idle is measured against the PTY typescript file's mtime: any output from
# Claude or its MCP plugins (telegram inbound, replies, status renders) causes
# `script -f` to flush the file, which bumps mtime. ACTIVITY_MARKER is only
# used as a session-start fallback before script has written its first byte.
touch_activity() { touch "${ACTIVITY_MARKER}"; }

get_idle_sec() {
    local pty_mtime
    pty_mtime=$(stat -c %Y "${PTY_LOG}" 2>/dev/null || echo 0)
    local marker_mtime
    marker_mtime=$(stat -c %Y "${ACTIVITY_MARKER}" 2>/dev/null || echo 0)
    local last_activity=$pty_mtime
    [ "$marker_mtime" -gt "$last_activity" ] && last_activity=$marker_mtime
    local now
    now=$(date +%s)
    echo $(( now - last_activity ))
}

# --- Memory check ---
get_memory_mb() {
    local mem_bytes
    mem_bytes=$(systemctl show gobrrr-channels --property=MemoryCurrent --value 2>/dev/null || echo 0)
    echo $(( mem_bytes / 1048576 ))
}

# --- Monitor (runs in background, kills script child of wrapper on threshold) ---
# Finds the script process by looking for children of the wrapper shell ($PPID
# from the monitor's perspective). We can't take the script PID directly because
# script runs in the foreground of the wrapper for PTY reasons (see main()).
monitor_by_parent() {
    local session_start=$1
    local target_pid=""

    # Wait briefly for script to be spawned by the parent wrapper.
    for _ in 1 2 3 4 5; do
        target_pid=$(pgrep -P "$PPID" -x script || true)
        [ -n "$target_pid" ] && break
        sleep 1
    done
    [ -z "$target_pid" ] && return 0

    while kill -0 "$target_pid" 2>/dev/null; do
        sleep "$CHECK_INTERVAL"

        local idle_sec
        idle_sec=$(get_idle_sec)
        if [ "$idle_sec" -gt "$((IDLE_TIMEOUT_MIN * 60))" ]; then
            echo "[$(date)] Idle timeout reached (${idle_sec}s idle)"
            kill -TERM "$target_pid" 2>/dev/null || true
            return 0
        fi

        local mem_mb
        mem_mb=$(get_memory_mb)
        if [ "$mem_mb" -gt "$MEMORY_CEILING_MB" ]; then
            echo "[$(date)] Memory ceiling reached (${mem_mb}MB)"
            kill -TERM "$target_pid" 2>/dev/null || true
            return 0
        fi

        local uptime_sec
        uptime_sec=$(( $(date +%s) - session_start ))
        local uptime_hours=$(( uptime_sec / 3600 ))
        if [ "$uptime_hours" -ge "$MAX_UPTIME_HOURS" ]; then
            if [ "$idle_sec" -lt "$((IDLE_TIMEOUT_MIN * 60))" ]; then
                echo "[$(date)] Max uptime reached but session active, deferring rotation"
                continue
            fi
            echo "[$(date)] Max uptime reached (${uptime_hours}h)"
            kill -TERM "$target_pid" 2>/dev/null || true
            return 0
        fi
    done
}

# --- Main loop ---
main() {
    mkdir -p "$(dirname "$PTY_LOG")"

    local restart_count
    restart_count=$(load_restart_count)

    while true; do
        local session_start
        session_start=$(date +%s)

        echo "[$(date)] Starting Claude Code channel session (restart count: $restart_count)"
        touch_activity

        # Monitor runs in background, watching the script child of this shell.
        # script MUST run in the foreground: backgrounding it with `&` makes
        # bash redirect its stdin to /dev/null and detach it from a controlling
        # terminal, which breaks Claude Code's channel notification dispatcher
        # (inbound telegram messages silently never reach the session).
        monitor_by_parent "$session_start" &
        local monitor_pid=$!

        # Launch Claude Code with channel plugins via expect. expect gives
        # claude the PTY it needs for ink and answers the
        # --dangerously-load-development-channels confirmation dialog.
        #
        # Why the dev flag: for personal / Max accounts Claude's allowlist is
        # a remote GrowthBook ledger (src/services/mcp/channelAllowlist.ts),
        # and managed-settings.json only affects team/enterprise subs. The
        # only way to get gobrrr-telegram past the allowlist gate is to mark
        # the entry dev:true, which requires actually accepting DevChannelsDialog.
        #
        # Why expect over a piped CR: the dialog text "Enter to confirm"
        # contains ANSI escapes between words, so the literal match pattern
        # must be a contiguous fragment ("confirm"). Plain stdin piping
        # through `script` also failed because ink reads from the PTY after
        # raw-mode is enabled, swallowing bytes that arrived before that.
        set +e
        /usr/bin/expect >> "$PTY_LOG" 2>&1 <<EXPECT_EOF
set timeout -1
log_user 1
spawn -noecho $CLAUDE_BIN $CHANNELS
expect {
    "confirm" { send "\\r"; exp_continue }
    eof
}
catch wait result
exit [lindex \$result 3]
EXPECT_EOF
        local exit_code=$?
        set -e
        echo "[$(date)] Claude exited with code $exit_code"

        kill "$monitor_pid" 2>/dev/null || true
        wait "$monitor_pid" 2>/dev/null || true

        local session_duration=$(( $(date +%s) - session_start ))
        if [ "$session_duration" -gt 300 ]; then
            reset_restart_count
            restart_count=0
        else
            restart_count=$((restart_count + 1))
            save_restart_count "$restart_count"
        fi

        local backoff
        backoff=$(get_backoff_sec "$restart_count")

        if [ "$backoff" -eq -1 ]; then
            echo "[$(date)] Too many restarts ($restart_count). Stopping."
            send_telegram "gobrrr-channels stopped after $restart_count consecutive failures. Manual intervention needed."
            exit 1
        fi

        if [ "$restart_count" -ge 4 ]; then
            send_telegram "gobrrr-channels restarting (attempt $restart_count). Backing off ${backoff}s."
        fi

        echo "[$(date)] Backing off ${backoff}s before restart"
        sleep "$backoff"
    done
}

main "$@"
