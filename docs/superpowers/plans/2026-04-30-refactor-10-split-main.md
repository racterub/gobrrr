# Refactor #10 — Split `cmd/gobrrr/main.go` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split `daemon/cmd/gobrrr/main.go` (1064 LoC) into per-verb files and eliminate the 37 package-global flag-value vars by switching to `cmd.Flags().GetString(...)`-style flag reads. `main.go` shrinks to ~50 lines holding only `main()`, `rootCmd`, the `init()` that wires registers, and `newClient()`.

**Architecture:** Two commits.

- **Commit A (per-verb split):** Create `daemon/cmd/gobrrr/{daemon,task,gmail,gcal,memory,session,timer,skill,setup}.go`. Each file owns one cobra parent verb plus its subcommands and exposes `func register<Verb>(root *cobra.Command)`. Print helpers move to the verb file that uses them (`printTask`/`printTaskSummary` → `task.go`, `printMemoryEntry`/`printMemoryList` → `memory.go`, `printApprovalCard`/`parseSlugVersion` → `skill.go`). Top-level `approve`/`deny` (root-level approval verbs, distinct from `skill approve`/`skill deny`) live in `task.go` since they sit at the same root level as `submit`/`status`/etc. `main.go` shrinks to entrypoint + register calls + `newClient()`.
- **Commit B (flag-style standardization):** Replace every `cmd.Flags().StringVar(&pkgVar, ...)` (and `BoolVar`/`IntVar`) with the no-`Var` equivalent (`String`/`Bool`/`Int`). Read flags inside `RunE` via `cmd.Flags().GetString("name")` like the existing `timer` commands. Delete the now-unused package-level flag vars.

**Tech Stack:** `github.com/spf13/cobra` (already present). No new dependencies.

**Sequence position:** Refactor #10 of the structural batch (`docs/superpowers/specs/2026-04-26-structural-refactor-batch-design.md`). #13, #7, #9, and #8 are merged. After #10 ships, only #6a and #6b (atomic-write extraction + parent-dir fsync) remain in the batch.

**Branch:** `refactor/10-split-main` (cut from `master`).

---

## Scope

Current state of `daemon/cmd/gobrrr/main.go`:

- 1064 LoC — every cobra command, every flag binding, every print helper, and one big `init()` block in a single file.
- 37 package-global flag-value vars (e.g. `submitPrompt`, `gmailListUnread`, `gcalCreateTitle`) bound via `Flags().StringVar(&...)` and read directly inside the cobra `RunE` closure.
- The `timer` verb (added later) already uses the target style: `cmd.Flags().String("name", "", ...)` to declare and `cmd.Flags().GetString("name")` to read inside `RunE`.

Concrete global-var sites (line numbers in current `main.go`):

| Line(s) | Verb | Vars |
|---------|------|------|
| 110–117 | submit | `submitPrompt`, `submitReplyTo`, `submitPriority`, `submitAllowWrites`, `submitTimeout`, `submitWarm` |
| 156 | list | `listAll` |
| 268–273 | gmail list | `gmailListUnread`, `gmailListQuery`, `gmailListLimit`, `gmailListAccount` |
| 296 | gmail read | `gmailReadAccount` |
| 314–319 | gmail send | `gmailSendTo`, `gmailSendSubject`, `gmailSendBody`, `gmailSendAccount` |
| 338–341 | gmail reply | `gmailReplyBody`, `gmailReplyAccount` |
| 366 | gcal today | `gcalTodayAccount` |
| 383 | gcal week | `gcalWeekAccount` |
| 400 | gcal get | `gcalGetAccount` |
| 418–424 | gcal create | `gcalCreateTitle`, `gcalCreateStart`, `gcalCreateEnd`, `gcalCreateDescription`, `gcalCreateAccount` |
| 443–448 | gcal update | `gcalUpdateTitle`, `gcalUpdateStart`, `gcalUpdateEnd`, `gcalUpdateAccount` |
| 465 | gcal delete | `gcalDeleteAccount` |
| 488–492 | memory save | `memorySaveContent`, `memorySaveTags`, `memorySaveSource` |
| 520–522 | memory search | `memorySearchTags` |
| 552 | memory list | `memoryListLimit` |
| 723 | setup google-account | `setupGoogleAccountName` |

After this refactor:

- `main.go` ≤ 100 lines (target ~50). Holds only `main()`, `rootCmd`, `init()` calling each `register<Verb>(rootCmd)`, and `newClient()`.
- One file per top-level verb under `cmd/gobrrr/`.
- Zero package-level flag-value vars (verified by grep — see Task 7).
- All `--help` output bytewise-identical to master.

---

## File Structure

| File | New / Modify | Responsibility |
|------|--------------|----------------|
| `daemon/cmd/gobrrr/main.go` | Modify | `main()`, `rootCmd`, `init()` calling registers, `newClient()` |
| `daemon/cmd/gobrrr/daemon.go` | Create | `registerDaemon(root)` — daemon start/status |
| `daemon/cmd/gobrrr/task.go` | Create | `registerTask(root)` — submit/list/status/cancel/logs/approve/deny + `printTask`, `printTaskSummary` |
| `daemon/cmd/gobrrr/gmail.go` | Create | `registerGmail(root)` — gmail list/read/send/reply |
| `daemon/cmd/gobrrr/gcal.go` | Create | `registerGcal(root)` — gcal today/week/get/create/update/delete |
| `daemon/cmd/gobrrr/memory.go` | Create | `registerMemory(root)` — memory save/search/list/get/delete + `printMemoryEntry`, `printMemoryList` |
| `daemon/cmd/gobrrr/session.go` | Create | `registerSession(root)` — session status/start/stop/restart |
| `daemon/cmd/gobrrr/timer.go` | Create | `registerTimer(root)` — timer create/list/remove |
| `daemon/cmd/gobrrr/skill.go` | Create | `registerSkill(root)` — skill list/search/install/approve/deny/uninstall + `printApprovalCard`, `parseSlugVersion` |
| `daemon/cmd/gobrrr/setup.go` | Create | `registerSetup(root)` — setup, setup google-account |

The `cmd/gobrrr` package stays a single Go package (`package main`), so all unexported identifiers remain visible across the new files.

No tests are added for this refactor — `cmd/gobrrr` has no test file today, and the spec gates this work on `--help` parity, not new unit tests. The build/test/vet gate from the umbrella spec still applies.

---

## Phase 1 — Per-verb file split (Commit A)

### Task 1: Cut branch and capture `--help` baseline

**Files:** None modified yet. This task only produces a comparison fixture used by Tasks 4 and 7.

**Goal:** Lock in a byte-exact baseline of every `--help` output so Tasks 4 and 7 can confirm zero CLI-surface drift. Working from master (after #8 merged), so the baseline reflects shipped behavior.

- [ ] **Step 1: Confirm clean working tree on `master`**

Run:
```bash
cd /home/racterub/github/gobrrr
git status --porcelain
```
Expected: empty output. If anything is dirty, stop and report — the baseline must be captured from a clean master.

- [ ] **Step 2: Cut the feature branch**

Run:
```bash
cd /home/racterub/github/gobrrr
git checkout master
git pull --ff-only
git checkout -b refactor/10-split-main
```
Expected: switched to a new branch on top of latest `master`.

- [ ] **Step 3: Build the current binary**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build -o /tmp/gobrrr-baseline ./cmd/gobrrr/
```
Expected: clean build, binary at `/tmp/gobrrr-baseline`.

- [ ] **Step 4: Capture `--help` for every verb path**

Run:
```bash
mkdir -p /tmp/gobrrr-help-baseline
B=/tmp/gobrrr-baseline
H=/tmp/gobrrr-help-baseline
$B --help                          > $H/root.txt
$B daemon --help                   > $H/daemon.txt
$B daemon start --help             > $H/daemon-start.txt
$B daemon status --help            > $H/daemon-status.txt
$B submit --help                   > $H/submit.txt
$B list --help                     > $H/list.txt
$B status --help                   > $H/task-status.txt
$B cancel --help                   > $H/cancel.txt
$B logs --help                     > $H/logs.txt
$B approve --help                  > $H/approve.txt
$B deny --help                     > $H/deny.txt
$B gmail --help                    > $H/gmail.txt
$B gmail list --help               > $H/gmail-list.txt
$B gmail read --help               > $H/gmail-read.txt
$B gmail send --help               > $H/gmail-send.txt
$B gmail reply --help              > $H/gmail-reply.txt
$B gcal --help                     > $H/gcal.txt
$B gcal today --help               > $H/gcal-today.txt
$B gcal week --help                > $H/gcal-week.txt
$B gcal get --help                 > $H/gcal-get.txt
$B gcal create --help              > $H/gcal-create.txt
$B gcal update --help              > $H/gcal-update.txt
$B gcal delete --help              > $H/gcal-delete.txt
$B memory --help                   > $H/memory.txt
$B memory save --help              > $H/memory-save.txt
$B memory search --help            > $H/memory-search.txt
$B memory list --help              > $H/memory-list.txt
$B memory get --help               > $H/memory-get.txt
$B memory delete --help            > $H/memory-delete.txt
$B session --help                  > $H/session.txt
$B session status --help           > $H/session-status.txt
$B session start --help            > $H/session-start.txt
$B session stop --help             > $H/session-stop.txt
$B session restart --help          > $H/session-restart.txt
$B timer --help                    > $H/timer.txt
$B timer create --help             > $H/timer-create.txt
$B timer list --help               > $H/timer-list.txt
$B timer remove --help             > $H/timer-remove.txt
$B skill --help                    > $H/skill.txt
$B skill list --help               > $H/skill-list.txt
$B skill search --help             > $H/skill-search.txt
$B skill install --help            > $H/skill-install.txt
$B skill approve --help            > $H/skill-approve.txt
$B skill deny --help               > $H/skill-deny.txt
$B skill uninstall --help          > $H/skill-uninstall.txt
$B setup --help                    > $H/setup.txt
$B setup google-account --help     > $H/setup-google-account.txt
ls $H | wc -l
```
Expected: `47` (47 captured help texts). If a help command exits non-zero or produces empty output, fix it before continuing — the baseline is load-bearing.

### Task 2: Extract small verbs (`daemon`, `session`, `timer`, `setup`)

**Files:**
- Create: `daemon/cmd/gobrrr/daemon.go`
- Create: `daemon/cmd/gobrrr/session.go`
- Create: `daemon/cmd/gobrrr/timer.go`
- Create: `daemon/cmd/gobrrr/setup.go`
- Modify: `daemon/cmd/gobrrr/main.go` (delete the moved blocks; do **not** yet wire registers — Task 4 does that as part of the main.go shrink)

**Goal:** Move the four lighter verbs out of `main.go` first. Each gets a `register<Verb>(root *cobra.Command)` function that builds the cobra commands locally (using closure-captured cobra command vars) and attaches them. Vars that today live at package scope and only support these verbs (`setupGoogleAccountName`) move into the corresponding new file as package-level vars **for now** — Phase 2 deletes them.

This task ends with `cmd/gobrrr` not yet building, because `main.go` still contains the old `init()` block referencing identifiers that just moved. That's acceptable mid-task; Task 4 fixes it before commit. Do not commit between tasks 2/3/4 — commit lands at the end of Task 4.

- [ ] **Step 1: Create `daemon/cmd/gobrrr/daemon.go`**

Create the file with this exact content:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/config"
	"github.com/racterub/gobrrr/internal/daemon"
)

// registerDaemon wires the `daemon` verb (start/status) onto root.
func registerDaemon(root *cobra.Command) {
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the gobrrr daemon",
	}

	daemonStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the gobrrr daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			gobrrDir := config.GobrrDir()
			if err := os.MkdirAll(gobrrDir, 0700); err != nil {
				return fmt.Errorf("creating gobrrr dir: %w", err)
			}

			cfg, err := config.Load(filepath.Join(gobrrDir, "config.json"))
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			socketPath := cfg.SocketPath
			if socketPath == "" {
				socketPath = filepath.Join(gobrrDir, "gobrrr.sock")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigs
				cancel()
			}()

			fmt.Fprintf(os.Stderr, "gobrrr daemon starting on %s\n", socketPath)
			d := daemon.New(cfg, socketPath)
			return d.Run(ctx)
		},
	}

	daemonStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			info, err := c.Health()
			if err != nil {
				return fmt.Errorf("daemon unreachable: %w", err)
			}
			fmt.Printf("Status:          %v\n", info["status"])
			fmt.Printf("Uptime (sec):    %v\n", info["uptime_sec"])
			fmt.Printf("Workers active:  %v\n", info["workers_active"])
			fmt.Printf("Queue depth:     %v\n", info["queue_depth"])
			if ww, ok := info["warm_workers"].(map[string]any); ok {
				fmt.Printf("Warm workers:    total=%.0f, ready=%.0f, busy=%.0f, disabled=%.0f\n",
					ww["total"], ww["ready"], ww["busy"], ww["disabled"])
			}
			if models, ok := info["models"].(map[string]any); ok {
				fmt.Println("Models:")
				for _, role := range []string{"launcher", "warm_worker", "cold_worker"} {
					if m, ok := models[role].(map[string]any); ok {
						fmt.Printf("  %-12s %v (%v)\n", role, m["model"], m["permission_mode"])
					}
				}
			}
			return nil
		},
	}

	daemonCmd.AddCommand(daemonStartCmd, daemonStatusCmd)
	root.AddCommand(daemonCmd)
}
```

- [ ] **Step 2: Create `daemon/cmd/gobrrr/session.go`**

Create the file with this exact content:

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// registerSession wires the `session` verb (status/start/stop/restart) onto root.
func registerSession(root *cobra.Command) {
	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage the Telegram channel session",
	}

	sessionStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show session status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			status, err := c.SessionStatus()
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(status, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	sessionStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Telegram session",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.SessionStart(); err != nil {
				return err
			}
			fmt.Println("Session starting")
			return nil
		},
	}

	sessionStopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the Telegram session",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.SessionStop(); err != nil {
				return err
			}
			fmt.Println("Session stopped")
			return nil
		},
	}

	sessionRestartCmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the Telegram session",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.SessionRestart(); err != nil {
				return err
			}
			fmt.Println("Session restarting")
			return nil
		},
	}

	sessionCmd.AddCommand(sessionStatusCmd, sessionStartCmd, sessionStopCmd, sessionRestartCmd)
	root.AddCommand(sessionCmd)
}
```

- [ ] **Step 3: Create `daemon/cmd/gobrrr/timer.go`**

Create the file with this exact content:

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// registerTimer wires the `timer` verb (create/list/remove) onto root.
func registerTimer(root *cobra.Command) {
	timerCmd := &cobra.Command{
		Use:   "timer",
		Short: "Manage scheduled tasks",
	}

	timerCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a recurring scheduled task",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			cronExpr, _ := cmd.Flags().GetString("cron")
			prompt, _ := cmd.Flags().GetString("prompt")
			replyTo, _ := cmd.Flags().GetString("reply-to")
			allowWrites, _ := cmd.Flags().GetBool("allow-writes")

			c := newClient()
			result, err := c.CreateSchedule(name, cronExpr, prompt, replyTo, allowWrites)
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	timerListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all scheduled tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			schedules, err := c.ListSchedules()
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(schedules, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}

	timerRemoveCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a scheduled task",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			c := newClient()
			if err := c.RemoveSchedule(name); err != nil {
				return err
			}
			fmt.Printf("Removed schedule %q\n", name)
			return nil
		},
	}

	timerCreateCmd.Flags().String("name", "", "Schedule name (required)")
	timerCreateCmd.Flags().String("cron", "", "Cron expression (required)")
	timerCreateCmd.Flags().String("prompt", "", "Task prompt (required)")
	timerCreateCmd.Flags().String("reply-to", "channel", "Result destination")
	timerCreateCmd.Flags().Bool("allow-writes", false, "Allow write operations")
	timerCreateCmd.MarkFlagRequired("name")
	timerCreateCmd.MarkFlagRequired("cron")
	timerCreateCmd.MarkFlagRequired("prompt")

	timerRemoveCmd.Flags().String("name", "", "Schedule name (required)")
	timerRemoveCmd.MarkFlagRequired("name")

	timerCmd.AddCommand(timerCreateCmd, timerListCmd, timerRemoveCmd)
	root.AddCommand(timerCmd)
}
```

- [ ] **Step 4: Create `daemon/cmd/gobrrr/setup.go`**

The `setup` verb still uses a package-level var (`setupGoogleAccountName`) in this commit. Phase 2 removes it. For now, keep the same shape — `register<Verb>` declares the var inside the function via closure capture so it stays out of package scope.

Create the file with this exact content:

```go
package main

import (
	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/setup"
)

// registerSetup wires the `setup` verb (and its `google-account` subcommand) onto root.
func registerSetup(root *cobra.Command) {
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Run first-time setup wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.RunWizard()
		},
	}

	var setupGoogleAccountName string

	setupGoogleAccountCmd := &cobra.Command{
		Use:   "google-account",
		Short: "Add a Google account",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.RunGoogleAccountSetup(setupGoogleAccountName)
		},
	}

	setupGoogleAccountCmd.Flags().StringVar(&setupGoogleAccountName, "name", "", "Account label (e.g. personal, work)")
	setupCmd.AddCommand(setupGoogleAccountCmd)
	root.AddCommand(setupCmd)
}
```

(Phase 2 will rewrite the `StringVar` line to `String("name", "", ...)` and read it via `cmd.Flags().GetString("name")` inside `RunE`. Keeping it bound to a closure-local var here preserves Commit A as a pure structural move with zero behavior change.)

- [ ] **Step 5: Delete the now-relocated blocks from `main.go`**

In `daemon/cmd/gobrrr/main.go`, delete these source ranges (line numbers from current master):

| Lines | Content |
|-------|---------|
| 38–106 | `daemonCmd`, `daemonStartCmd`, `daemonStatusCmd` |
| 597–656 | `sessionCmd` and its 4 subcommands |
| 658–713 | `timerCmd` and its 3 subcommands |
| 715–731 | `setupCmd`, `setupGoogleAccountName`, `setupGoogleAccountCmd` |

The `init()` block (currently lines 897–1005) still references the deleted identifiers. **Don't touch `init()` yet** — Task 4 rewrites it wholesale.

- [ ] **Step 6: Stage but do not commit**

Run:
```bash
cd /home/racterub/github/gobrrr
git add daemon/cmd/gobrrr/daemon.go daemon/cmd/gobrrr/session.go daemon/cmd/gobrrr/timer.go daemon/cmd/gobrrr/setup.go daemon/cmd/gobrrr/main.go
git status --short
```
Expected: `A` for the four new files, `M` for `main.go`. No commit yet.

### Task 3: Extract heavier verbs (`task`, `gmail`, `gcal`, `memory`, `skill`)

**Files:**
- Create: `daemon/cmd/gobrrr/task.go`
- Create: `daemon/cmd/gobrrr/gmail.go`
- Create: `daemon/cmd/gobrrr/gcal.go`
- Create: `daemon/cmd/gobrrr/memory.go`
- Create: `daemon/cmd/gobrrr/skill.go`
- Modify: `daemon/cmd/gobrrr/main.go` (delete the moved blocks; `init()` still untouched)

**Goal:** Move the remaining five verbs. Print helpers travel with their consumers (`printTask`/`printTaskSummary` → `task.go`, `printMemoryEntry`/`printMemoryList` → `memory.go`, `printApprovalCard`/`parseSlugVersion` → `skill.go`). Top-level `approve`/`deny` (root-level) live in `task.go` because they sit at root level alongside `submit`/`list`/`status`/`cancel`/`logs`. Package-level flag vars **stay package-level** in this task — Phase 2 deletes them.

This task ends with `cmd/gobrrr` still not building; Task 4 closes the loop.

- [ ] **Step 1: Create `daemon/cmd/gobrrr/task.go`**

This file holds: `submit`, `list`, `status` (task status, distinct from `daemon status`), `cancel`, `logs`, top-level `approve`, top-level `deny`, plus `printTask` / `printTaskSummary`. Flag vars (`submitPrompt` etc., `listAll`) stay at package scope — Phase 2 strips them.

Create the file with this exact content:

```go
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/daemon"
)

// Submit flag-value vars (Phase 2 will eliminate these in favor of cmd.Flags().Get*).
var (
	submitPrompt      string
	submitReplyTo     string
	submitPriority    int
	submitAllowWrites bool
	submitTimeout     int
	submitWarm        bool
)

// List flag-value var (Phase 2 will eliminate this).
var listAll bool

// registerTask wires the root-level task verbs (submit/list/status/cancel/logs/approve/deny) onto root.
func registerTask(root *cobra.Command) {
	submitCmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a new task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if submitPrompt == "" {
				return fmt.Errorf("--prompt is required")
			}
			c := newClient()
			task, err := c.SubmitTask(submitPrompt, submitReplyTo, submitPriority, submitAllowWrites, submitTimeout, submitWarm)
			if err != nil {
				return err
			}

			// When reply-to is stdout, block until the task completes and print
			// the result to stdout (or error to stderr with non-zero exit).
			if submitReplyTo == "stdout" {
				result, waitErr := c.WaitForTask(task.ID)
				if waitErr != nil {
					// Connection loss: exit 2; task failure: exit 1.
					if strings.Contains(waitErr.Error(), "daemon connection lost") {
						fmt.Fprintln(os.Stderr, waitErr.Error())
						os.Exit(2)
					}
					fmt.Fprintln(os.Stderr, waitErr.Error())
					os.Exit(1)
				}
				fmt.Print(result)
				return nil
			}

			printTask(task)
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			tasks, err := c.ListTasks(listAll)
			if err != nil {
				return err
			}
			if len(tasks) == 0 {
				fmt.Println("No tasks.")
				return nil
			}
			for _, t := range tasks {
				printTaskSummary(t)
			}
			return nil
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status <id>",
		Short: "Show task status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			task, err := c.GetTask(args[0])
			if err != nil {
				return err
			}
			printTask(task)
			return nil
		},
	}

	cancelCmd := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.CancelTask(args[0]); err != nil {
				return err
			}
			fmt.Printf("Task %s cancelled.\n", args[0])
			return nil
		},
	}

	logsCmd := &cobra.Command{
		Use:   "logs <id>",
		Short: "Show task logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			logs, err := c.GetLogs(args[0])
			if err != nil {
				return err
			}
			fmt.Print(logs)
			return nil
		},
	}

	// Top-level approve/deny act on any pending approval id (skill_install today,
	// write_action soon). Per-kind subcommands like `gobrrr skill approve` still
	// exist when a kind needs extra options (e.g. --skip-binary).
	approveCmd := &cobra.Command{
		Use:   "approve <approval-id>",
		Short: "Approve a pending approval request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().DecideApproval(args[0], "approve"); err != nil {
				return err
			}
			fmt.Println("approved")
			return nil
		},
	}

	denyCmd := &cobra.Command{
		Use:   "deny <approval-id>",
		Short: "Deny a pending approval request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().DecideApproval(args[0], "deny"); err != nil {
				return err
			}
			fmt.Println("denied")
			return nil
		},
	}

	submitCmd.Flags().StringVar(&submitPrompt, "prompt", "", "Task prompt (required)")
	submitCmd.Flags().StringVar(&submitReplyTo, "reply-to", "channel", "Reply destination (e.g. channel, telegram, stdout)")
	submitCmd.Flags().IntVar(&submitPriority, "priority", 5, "Task priority (lower = higher priority)")
	submitCmd.Flags().BoolVar(&submitAllowWrites, "allow-writes", false, "Allow file writes")
	submitCmd.Flags().IntVar(&submitTimeout, "timeout", 300, "Timeout in seconds")
	submitCmd.Flags().BoolVar(&submitWarm, "warm", false, "Route to warm worker for fast dispatch")

	listCmd.Flags().BoolVar(&listAll, "all", false, "Include completed/failed tasks")

	root.AddCommand(submitCmd, listCmd, statusCmd, cancelCmd, logsCmd, approveCmd, denyCmd)
}

// printTask prints full task details.
func printTask(t *daemon.Task) {
	fmt.Printf("ID:          %s\n", t.ID)
	fmt.Printf("Status:      %s\n", t.Status)
	fmt.Printf("Priority:    %d\n", t.Priority)
	fmt.Printf("Prompt:      %s\n", t.Prompt)
	fmt.Printf("Reply-To:    %s\n", t.ReplyTo)
	fmt.Printf("Allow Writes:%v\n", t.AllowWrites)
	fmt.Printf("Created:     %s\n", t.CreatedAt.Format("2006-01-02 15:04:05"))
	if t.StartedAt != nil {
		fmt.Printf("Started:     %s\n", t.StartedAt.Format("2006-01-02 15:04:05"))
	}
	if t.CompletedAt != nil {
		fmt.Printf("Completed:   %s\n", t.CompletedAt.Format("2006-01-02 15:04:05"))
	}
	if t.Result != nil {
		fmt.Printf("Result:      %s\n", *t.Result)
	}
	if t.Error != nil {
		fmt.Printf("Error:       %s\n", *t.Error)
	}
}

// printTaskSummary prints a one-line summary.
func printTaskSummary(t *daemon.Task) {
	fmt.Printf("%-26s  %-10s  p%-3d  %s\n", t.ID, t.Status, t.Priority, t.Prompt)
}
```

- [ ] **Step 2: Create `daemon/cmd/gobrrr/gmail.go`**

Create the file with this exact content:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Gmail flag-value vars (Phase 2 will eliminate these).
var (
	gmailListUnread   bool
	gmailListQuery    string
	gmailListLimit    int
	gmailListAccount  string
	gmailReadAccount  string
	gmailSendTo       string
	gmailSendSubject  string
	gmailSendBody     string
	gmailSendAccount  string
	gmailReplyBody    string
	gmailReplyAccount string
)

// registerGmail wires the `gmail` verb (list/read/send/reply) onto root.
func registerGmail(root *cobra.Command) {
	gmailCmd := &cobra.Command{
		Use:   "gmail",
		Short: "Gmail integration",
	}

	gmailListCmd := &cobra.Command{
		Use:   "list",
		Short: "List Gmail messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			query := gmailListQuery
			if gmailListUnread && query == "" {
				query = "is:unread"
			} else if gmailListUnread {
				query = "is:unread " + query
			}
			c := newClient()
			result, err := c.GmailList(query, gmailListLimit, gmailListAccount, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gmailReadCmd := &cobra.Command{
		Use:   "read <message-id>",
		Short: "Read a Gmail message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			result, err := c.GmailRead(args[0], gmailReadAccount, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gmailSendCmd := &cobra.Command{
		Use:   "send",
		Short: "Send a Gmail message",
		RunE: func(cmd *cobra.Command, args []string) error {
			if gmailSendTo == "" {
				return fmt.Errorf("--to is required")
			}
			c := newClient()
			if err := c.GmailSend(gmailSendTo, gmailSendSubject, gmailSendBody, gmailSendAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Message sent.")
			return nil
		},
	}

	gmailReplyCmd := &cobra.Command{
		Use:   "reply <message-id>",
		Short: "Reply to a Gmail message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if gmailReplyBody == "" {
				return fmt.Errorf("--body is required")
			}
			c := newClient()
			if err := c.GmailReply(args[0], gmailReplyBody, gmailReplyAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Reply sent.")
			return nil
		},
	}

	gmailListCmd.Flags().BoolVar(&gmailListUnread, "unread", false, "Filter to unread messages")
	gmailListCmd.Flags().StringVar(&gmailListQuery, "query", "", "Gmail search query")
	gmailListCmd.Flags().IntVar(&gmailListLimit, "limit", 10, "Maximum number of messages to return")
	gmailListCmd.Flags().StringVar(&gmailListAccount, "account", "default", "Account name")

	gmailReadCmd.Flags().StringVar(&gmailReadAccount, "account", "default", "Account name")

	gmailSendCmd.Flags().StringVar(&gmailSendTo, "to", "", "Recipient email address (required)")
	gmailSendCmd.Flags().StringVar(&gmailSendSubject, "subject", "", "Email subject")
	gmailSendCmd.Flags().StringVar(&gmailSendBody, "body", "", "Email body")
	gmailSendCmd.Flags().StringVar(&gmailSendAccount, "account", "default", "Account name")

	gmailReplyCmd.Flags().StringVar(&gmailReplyBody, "body", "", "Reply body (required)")
	gmailReplyCmd.Flags().StringVar(&gmailReplyAccount, "account", "default", "Account name")

	gmailCmd.AddCommand(gmailListCmd, gmailReadCmd, gmailSendCmd, gmailReplyCmd)
	root.AddCommand(gmailCmd)
}
```

- [ ] **Step 3: Create `daemon/cmd/gobrrr/gcal.go`**

Create the file with this exact content:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Gcal flag-value vars (Phase 2 will eliminate these).
var (
	gcalTodayAccount      string
	gcalWeekAccount       string
	gcalGetAccount        string
	gcalCreateTitle       string
	gcalCreateStart       string
	gcalCreateEnd         string
	gcalCreateDescription string
	gcalCreateAccount     string
	gcalUpdateTitle       string
	gcalUpdateStart       string
	gcalUpdateEnd         string
	gcalUpdateAccount     string
	gcalDeleteAccount     string
)

// registerGcal wires the `gcal` verb (today/week/get/create/update/delete) onto root.
func registerGcal(root *cobra.Command) {
	gcalCmd := &cobra.Command{
		Use:   "gcal",
		Short: "Google Calendar integration",
	}

	gcalTodayCmd := &cobra.Command{
		Use:   "today",
		Short: "List today's calendar events",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			result, err := c.GcalToday(gcalTodayAccount, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gcalWeekCmd := &cobra.Command{
		Use:   "week",
		Short: "List this week's calendar events",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			result, err := c.GcalWeek(gcalWeekAccount, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gcalGetCmd := &cobra.Command{
		Use:   "get <event-id>",
		Short: "Get a calendar event by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			result, err := c.GcalGetEvent(args[0], gcalGetAccount, os.Getenv("GOBRRR_TASK_ID"))
			if err != nil {
				return err
			}
			fmt.Print(result)
			return nil
		},
	}

	gcalCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a calendar event",
		RunE: func(cmd *cobra.Command, args []string) error {
			if gcalCreateTitle == "" {
				return fmt.Errorf("--title is required")
			}
			c := newClient()
			if err := c.GcalCreateEvent(gcalCreateTitle, gcalCreateStart, gcalCreateEnd, gcalCreateDescription, gcalCreateAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Event created.")
			return nil
		},
	}

	gcalUpdateCmd := &cobra.Command{
		Use:   "update <event-id>",
		Short: "Update a calendar event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.GcalUpdateEvent(args[0], gcalUpdateTitle, gcalUpdateStart, gcalUpdateEnd, gcalUpdateAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Println("Event updated.")
			return nil
		},
	}

	gcalDeleteCmd := &cobra.Command{
		Use:   "delete <event-id>",
		Short: "Delete a calendar event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.GcalDeleteEvent(args[0], gcalDeleteAccount, os.Getenv("GOBRRR_TASK_ID")); err != nil {
				return err
			}
			fmt.Printf("Event %s deleted.\n", args[0])
			return nil
		},
	}

	gcalTodayCmd.Flags().StringVar(&gcalTodayAccount, "account", "default", "Account name")
	gcalWeekCmd.Flags().StringVar(&gcalWeekAccount, "account", "default", "Account name")
	gcalGetCmd.Flags().StringVar(&gcalGetAccount, "account", "default", "Account name")

	gcalCreateCmd.Flags().StringVar(&gcalCreateTitle, "title", "", "Event title (required)")
	gcalCreateCmd.Flags().StringVar(&gcalCreateStart, "start", "", "Event start time (RFC3339)")
	gcalCreateCmd.Flags().StringVar(&gcalCreateEnd, "end", "", "Event end time (RFC3339)")
	gcalCreateCmd.Flags().StringVar(&gcalCreateDescription, "description", "", "Event description")
	gcalCreateCmd.Flags().StringVar(&gcalCreateAccount, "account", "default", "Account name")

	gcalUpdateCmd.Flags().StringVar(&gcalUpdateTitle, "title", "", "New event title")
	gcalUpdateCmd.Flags().StringVar(&gcalUpdateStart, "start", "", "New event start time (RFC3339)")
	gcalUpdateCmd.Flags().StringVar(&gcalUpdateEnd, "end", "", "New event end time (RFC3339)")
	gcalUpdateCmd.Flags().StringVar(&gcalUpdateAccount, "account", "default", "Account name")

	gcalDeleteCmd.Flags().StringVar(&gcalDeleteAccount, "account", "default", "Account name")

	gcalCmd.AddCommand(gcalTodayCmd, gcalWeekCmd, gcalGetCmd, gcalCreateCmd, gcalUpdateCmd, gcalDeleteCmd)
	root.AddCommand(gcalCmd)
}
```

- [ ] **Step 4: Create `daemon/cmd/gobrrr/memory.go`**

Create the file with this exact content:

```go
package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/memory"
)

// Memory flag-value vars (Phase 2 will eliminate these).
var (
	memorySaveContent string
	memorySaveTags    string
	memorySaveSource  string
	memorySearchTags  string
	memoryListLimit   int
)

// registerMemory wires the `memory` verb (save/search/list/get/delete) onto root.
func registerMemory(root *cobra.Command) {
	memoryCmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage daemon memory",
	}

	memorySaveCmd := &cobra.Command{
		Use:   "save",
		Short: "Save a memory entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if memorySaveContent == "" {
				return fmt.Errorf("--content is required")
			}
			var tags []string
			if memorySaveTags != "" {
				for _, t := range strings.Split(memorySaveTags, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}
			c := newClient()
			entry, err := c.SaveMemory(memorySaveContent, tags, memorySaveSource)
			if err != nil {
				return err
			}
			printMemoryEntry(entry)
			return nil
		},
	}

	memorySearchCmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search memory entries",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			var tags []string
			if memorySearchTags != "" {
				for _, t := range strings.Split(memorySearchTags, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}
			c := newClient()
			entries, err := c.SearchMemory(query, tags, 0)
			if err != nil {
				return err
			}
			printMemoryList(entries)
			return nil
		},
	}

	memoryListCmd := &cobra.Command{
		Use:   "list",
		Short: "List memory entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			entries, err := c.SearchMemory("", nil, memoryListLimit)
			if err != nil {
				return err
			}
			printMemoryList(entries)
			return nil
		},
	}

	memoryGetCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a memory entry by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			entry, err := c.GetMemory(args[0])
			if err != nil {
				return err
			}
			printMemoryEntry(entry)
			return nil
		},
	}

	memoryDeleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a memory entry by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.DeleteMemory(args[0]); err != nil {
				return err
			}
			fmt.Printf("Memory %s deleted.\n", args[0])
			return nil
		},
	}

	memorySaveCmd.Flags().StringVar(&memorySaveContent, "content", "", "Memory content (required)")
	memorySaveCmd.Flags().StringVar(&memorySaveTags, "tags", "", "Comma-separated tags")
	memorySaveCmd.Flags().StringVar(&memorySaveSource, "source", "", "Source of the memory")

	memorySearchCmd.Flags().StringVar(&memorySearchTags, "tags", "", "Comma-separated tags to filter by")

	memoryListCmd.Flags().IntVar(&memoryListLimit, "limit", 20, "Maximum number of entries to return")

	memoryCmd.AddCommand(memorySaveCmd, memorySearchCmd, memoryListCmd, memoryGetCmd, memoryDeleteCmd)
	root.AddCommand(memoryCmd)
}

// printMemoryEntry prints full details of a memory entry.
func printMemoryEntry(e *memory.Entry) {
	fmt.Printf("ID:         %s\n", e.ID)
	fmt.Printf("Source:     %s\n", e.Source)
	fmt.Printf("Tags:       %s\n", strings.Join(e.Tags, ", "))
	fmt.Printf("Created:    %s\n", e.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Content:    %s\n", e.Content)
}

// printMemoryList prints a compact list of memory entries.
func printMemoryList(entries []*memory.Entry) {
	if len(entries) == 0 {
		fmt.Println("No memory entries.")
		return
	}
	for _, e := range entries {
		summary := e.Content
		if len(summary) > 60 {
			summary = summary[:60] + "..."
		}
		fmt.Printf("%-30s  %-20s  %s\n", e.ID, strings.Join(e.Tags, ","), summary)
	}
}
```

- [ ] **Step 5: Create `daemon/cmd/gobrrr/skill.go`**

Create the file with this exact content:

```go
package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/client"
	"github.com/racterub/gobrrr/internal/skills"
)

// registerSkill wires the `skill` verb (list/search/install/approve/deny/uninstall) onto root.
func registerSkill(root *cobra.Command) {
	skillCmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage worker skills",
	}

	skillListCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli := newClient()
			list, err := cli.ListSkills()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("No skills installed.")
				return nil
			}
			byType := map[string][]skills.Skill{}
			for _, s := range list {
				byType[string(s.Type)] = append(byType[string(s.Type)], s)
			}
			for _, t := range []string{"system", "clawhub", "user"} {
				if len(byType[t]) == 0 {
					continue
				}
				fmt.Printf("[%s]\n", t)
				for _, s := range byType[t] {
					fmt.Printf("  %-20s  %s\n", s.Slug, s.Description)
				}
			}
			return nil
		},
	}

	skillSearchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search ClawHub for skills",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := newClient().SearchSkills(args[0])
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("No matches.")
				return nil
			}
			for _, r := range results {
				summary := ""
				if r.Summary != nil {
					summary = *r.Summary
				}
				fmt.Printf("%-24s  %s\n", r.Slug, summary)
			}
			return nil
		},
	}

	skillInstallCmd := &cobra.Command{
		Use:   "install <slug>[@version]",
		Short: "Stage a ClawHub skill install (prints approval card)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, version := parseSlugVersion(args[0])
			res, err := newClient().InstallSkill(slug, version)
			if err != nil {
				return err
			}
			printApprovalCard(res)
			return nil
		},
	}

	skillApproveCmd := &cobra.Command{
		Use:   "approve <request-id>",
		Short: "Approve a staged skill install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skip, _ := cmd.Flags().GetBool("skip-binary")
			decision := "approve"
			if skip {
				decision = "skip_binary"
			}
			if err := newClient().DecideApproval(args[0], decision); err != nil {
				return err
			}
			fmt.Println("approved")
			return nil
		},
	}

	skillDenyCmd := &cobra.Command{
		Use:   "deny <request-id>",
		Short: "Deny a staged skill install",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().DecideApproval(args[0], "deny"); err != nil {
				return err
			}
			fmt.Println("denied")
			return nil
		},
	}

	skillUninstallCmd := &cobra.Command{
		Use:   "uninstall <slug>",
		Short: "Uninstall a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().UninstallSkill(args[0]); err != nil {
				return err
			}
			fmt.Printf("uninstalled %s\n", args[0])
			return nil
		},
	}

	skillApproveCmd.Flags().Bool("skip-binary", false, "approve skill only, skip binary install commands")
	skillCmd.AddCommand(skillListCmd, skillSearchCmd, skillInstallCmd, skillApproveCmd, skillDenyCmd, skillUninstallCmd)
	root.AddCommand(skillCmd)
}

func parseSlugVersion(s string) (string, string) {
	if i := strings.Index(s, "@"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func printApprovalCard(r *client.InstallResult) {
	req := r.Request
	fmt.Printf("Install skill: %s@%s\n", req.Slug, req.Version)
	fmt.Printf("  Source: %s\n  sha256: %s\n", req.SourceURL, req.SHA256)
	if req.Frontmatter.Description != "" {
		fmt.Printf("  Description: %s\n", req.Frontmatter.Description)
	}
	fmt.Println()

	if len(req.MissingBins) > 0 {
		fmt.Printf("  Requires binaries: %s  (not on PATH)\n", strings.Join(req.MissingBins, ", "))
		for _, p := range req.ProposedCommands {
			fmt.Printf("    Proposed install:  %s\n", p.Command)
		}
		fmt.Println()
	}

	reads := req.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions.Read
	writes := req.Frontmatter.Metadata.OpenClaw.Requires.ToolPermissions.Write
	if len(reads) > 0 {
		fmt.Println("  Tool permissions (read, always allowed):")
		for _, p := range reads {
			fmt.Printf("    %s\n", p)
		}
	}
	if len(writes) > 0 {
		fmt.Println("\n  Tool permissions (write, require --allow-writes on task):")
		for _, p := range writes {
			fmt.Printf("    %s\n", p)
		}
	}

	fmt.Printf("\n  Request ID: %s\n\n", r.RequestID)
	fmt.Printf("  To proceed:  gobrrr skill approve %s\n", r.RequestID)
	fmt.Printf("  Skill only:  gobrrr skill approve %s --skip-binary\n", r.RequestID)
	fmt.Printf("  Cancel:      gobrrr skill deny %s\n", r.RequestID)
	fmt.Printf("  (Inline approval also available via Telegram once the bot is running.)\n")
}
```

- [ ] **Step 6: Delete the now-relocated blocks from `main.go`**

In `daemon/cmd/gobrrr/main.go`, delete these source ranges (line numbers from current master):

| Lines | Content |
|-------|---------|
| 108–258 | submit/list/status/cancel/logs/approve/deny + their flag-var blocks |
| 260–479 | gmail and gcal verbs + flag vars |
| 481–595 | memory verb + flag vars |
| 733–895 | skill verb + `parseSlugVersion` + `printApprovalCard` |
| 1014–1063 | `printTask`, `printTaskSummary`, `printMemoryEntry`, `printMemoryList` |

After this step, `main.go` should still contain `package main`, the imports block, `main()`, `rootCmd`, the `init()` block (still wired with the **old** style — Task 4 rewrites it), and `newClient()`.

- [ ] **Step 7: Stage but do not commit**

Run:
```bash
cd /home/racterub/github/gobrrr
git add daemon/cmd/gobrrr/task.go daemon/cmd/gobrrr/gmail.go daemon/cmd/gobrrr/gcal.go daemon/cmd/gobrrr/memory.go daemon/cmd/gobrrr/skill.go daemon/cmd/gobrrr/main.go
git status --short
```
Expected: 5 new files staged, `main.go` modified. No commit.

### Task 4: Shrink `main.go`, build/test, diff `--help`, commit A

**Files:**
- Modify: `daemon/cmd/gobrrr/main.go`

**Goal:** Replace the large `init()` block with calls to each `register<Verb>(rootCmd)`. Trim the imports to the minimum still needed (`config`, `cobra`, `fmt`, `os`, `path/filepath`, `client`). Verify the build, the test suite, and that every `--help` output matches the baseline. Commit A.

- [ ] **Step 1: Rewrite `main.go` to its final shape**

Replace the **entire** content of `daemon/cmd/gobrrr/main.go` with this exact text:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/racterub/gobrrr/internal/client"
	"github.com/racterub/gobrrr/internal/config"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "gobrrr",
	Short: "gobrrr — a Go daemon that dispatches Claude Code tasks",
	Long: `gobrrr is a daemon that dispatches Claude Code tasks with built-in
Gmail and Calendar integration. It listens on a Unix socket and
spawns claude workers to execute approved tasks.`,
}

func init() {
	registerDaemon(rootCmd)
	registerTask(rootCmd)
	registerGmail(rootCmd)
	registerGcal(rootCmd)
	registerMemory(rootCmd)
	registerSession(rootCmd)
	registerTimer(rootCmd)
	registerSkill(rootCmd)
	registerSetup(rootCmd)
}

// newClient creates a Client connected to the configured socket path.
func newClient() *client.Client {
	gobrrDir := config.GobrrDir()
	socketPath := filepath.Join(gobrrDir, "gobrrr.sock")
	return client.New(socketPath)
}
```

Note that `cobra` is imported because `rootCmd` is a `*cobra.Command`.

- [ ] **Step 2: Verify the build**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./...
```
Expected: clean build.

- [ ] **Step 3: Verify `go vet` and `go test`**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
go vet ./...
go test ./...
```
Expected: vet emits nothing; all tests pass.

- [ ] **Step 4: Build the post-split binary and capture `--help` output**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build -o /tmp/gobrrr-after ./cmd/gobrrr/
mkdir -p /tmp/gobrrr-help-after
B=/tmp/gobrrr-after
H=/tmp/gobrrr-help-after
$B --help                          > $H/root.txt
$B daemon --help                   > $H/daemon.txt
$B daemon start --help             > $H/daemon-start.txt
$B daemon status --help            > $H/daemon-status.txt
$B submit --help                   > $H/submit.txt
$B list --help                     > $H/list.txt
$B status --help                   > $H/task-status.txt
$B cancel --help                   > $H/cancel.txt
$B logs --help                     > $H/logs.txt
$B approve --help                  > $H/approve.txt
$B deny --help                     > $H/deny.txt
$B gmail --help                    > $H/gmail.txt
$B gmail list --help               > $H/gmail-list.txt
$B gmail read --help               > $H/gmail-read.txt
$B gmail send --help               > $H/gmail-send.txt
$B gmail reply --help              > $H/gmail-reply.txt
$B gcal --help                     > $H/gcal.txt
$B gcal today --help               > $H/gcal-today.txt
$B gcal week --help                > $H/gcal-week.txt
$B gcal get --help                 > $H/gcal-get.txt
$B gcal create --help              > $H/gcal-create.txt
$B gcal update --help              > $H/gcal-update.txt
$B gcal delete --help              > $H/gcal-delete.txt
$B memory --help                   > $H/memory.txt
$B memory save --help              > $H/memory-save.txt
$B memory search --help            > $H/memory-search.txt
$B memory list --help              > $H/memory-list.txt
$B memory get --help               > $H/memory-get.txt
$B memory delete --help            > $H/memory-delete.txt
$B session --help                  > $H/session.txt
$B session status --help           > $H/session-status.txt
$B session start --help            > $H/session-start.txt
$B session stop --help             > $H/session-stop.txt
$B session restart --help          > $H/session-restart.txt
$B timer --help                    > $H/timer.txt
$B timer create --help             > $H/timer-create.txt
$B timer list --help               > $H/timer-list.txt
$B timer remove --help             > $H/timer-remove.txt
$B skill --help                    > $H/skill.txt
$B skill list --help               > $H/skill-list.txt
$B skill search --help             > $H/skill-search.txt
$B skill install --help            > $H/skill-install.txt
$B skill approve --help            > $H/skill-approve.txt
$B skill deny --help               > $H/skill-deny.txt
$B skill uninstall --help          > $H/skill-uninstall.txt
$B setup --help                    > $H/setup.txt
$B setup google-account --help     > $H/setup-google-account.txt
diff -ru /tmp/gobrrr-help-baseline /tmp/gobrrr-help-after
```
Expected: `diff` exits 0 with no output (every help text is byte-identical to master). If any file differs, **stop and fix** — Commit A is supposed to be a pure structural move.

- [ ] **Step 5: Sanity-check main.go LoC**

Run:
```bash
wc -l /home/racterub/github/gobrrr/daemon/cmd/gobrrr/main.go
```
Expected: a number ≤ 60. If it's larger, something didn't migrate. Investigate before committing.

- [ ] **Step 6: Commit A**

Run:
```bash
cd /home/racterub/github/gobrrr
git add daemon/cmd/gobrrr/main.go daemon/cmd/gobrrr/daemon.go daemon/cmd/gobrrr/task.go daemon/cmd/gobrrr/gmail.go daemon/cmd/gobrrr/gcal.go daemon/cmd/gobrrr/memory.go daemon/cmd/gobrrr/session.go daemon/cmd/gobrrr/timer.go daemon/cmd/gobrrr/skill.go daemon/cmd/gobrrr/setup.go
git status --short
git diff --cached --stat
```
Expected: 9 new files added, `main.go` modified.

Then create the commit:

```bash
git commit -m "$(cat <<'EOF'
refactor(cli): split main.go into per-verb files

Move every cobra parent verb into its own file under cmd/gobrrr/
(daemon, task, gmail, gcal, memory, session, timer, skill, setup).
Each file owns a register<Verb>(root *cobra.Command) function that
builds its commands and attaches them. main.go shrinks to just
main(), rootCmd, init() calling each register, and newClient().

Print helpers travel with their consumers: printTask/printTaskSummary
into task.go, printMemoryEntry/printMemoryList into memory.go,
printApprovalCard/parseSlugVersion into skill.go.

Pure structural change. Every --help output is byte-identical to the
prior master (verified by diff against pre-split baseline). Package-
level flag-value vars (submitPrompt, gmailListUnread, etc.) migrate
unchanged in this commit; the next commit standardizes them on
cmd.Flags().Get*-style reads to eliminate package globals.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
git log --oneline -1
```
Expected: one new commit on `refactor/10-split-main` titled `refactor(cli): split main.go into per-verb files`.

**Phase 1 complete. Stop here for `/compact` checkpoint.**

---

## Phase 2 — Standardize flag style (Commit B)

### Task 5: Standardize flags in `task`, `gmail`, `gcal`

**Files:**
- Modify: `daemon/cmd/gobrrr/task.go`
- Modify: `daemon/cmd/gobrrr/gmail.go`
- Modify: `daemon/cmd/gobrrr/gcal.go`

**Goal:** Replace every `Flags().StringVar(&pkgVar, ...)` with `Flags().String(...)` (likewise for `BoolVar`/`IntVar`), read flags inside `RunE` via `cmd.Flags().GetString("name")` (etc.), and **delete** the package-level flag vars at the top of each file. Behavior unchanged. The `timer` and existing `skill approve --skip-binary` already use this style — copy their pattern verbatim.

- [ ] **Step 1: Update `daemon/cmd/gobrrr/task.go`**

Make the following edits:

1. Delete the `var (\n submitPrompt string ...)` block (the 6-var submit block) and the `var listAll bool` declaration at the top of the file.
2. Replace the body of the `submitCmd` `RunE` so it reads flags inside the closure. Specifically, change the block that begins `if submitPrompt == "" {` so it now begins with:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    prompt, _ := cmd.Flags().GetString("prompt")
    replyTo, _ := cmd.Flags().GetString("reply-to")
    priority, _ := cmd.Flags().GetInt("priority")
    allowWrites, _ := cmd.Flags().GetBool("allow-writes")
    timeout, _ := cmd.Flags().GetInt("timeout")
    warm, _ := cmd.Flags().GetBool("warm")

    if prompt == "" {
        return fmt.Errorf("--prompt is required")
    }
    c := newClient()
    task, err := c.SubmitTask(prompt, replyTo, priority, allowWrites, timeout, warm)
    if err != nil {
        return err
    }

    // When reply-to is stdout, block until the task completes and print
    // the result to stdout (or error to stderr with non-zero exit).
    if replyTo == "stdout" {
        result, waitErr := c.WaitForTask(task.ID)
        if waitErr != nil {
            // Connection loss: exit 2; task failure: exit 1.
            if strings.Contains(waitErr.Error(), "daemon connection lost") {
                fmt.Fprintln(os.Stderr, waitErr.Error())
                os.Exit(2)
            }
            fmt.Fprintln(os.Stderr, waitErr.Error())
            os.Exit(1)
        }
        fmt.Print(result)
        return nil
    }

    printTask(task)
    return nil
},
```

3. Replace the body of `listCmd.RunE` so it reads `all`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    all, _ := cmd.Flags().GetBool("all")
    c := newClient()
    tasks, err := c.ListTasks(all)
    if err != nil {
        return err
    }
    if len(tasks) == 0 {
        fmt.Println("No tasks.")
        return nil
    }
    for _, t := range tasks {
        printTaskSummary(t)
    }
    return nil
},
```

4. Replace the flag-binding block at the bottom of `registerTask`:

```go
submitCmd.Flags().String("prompt", "", "Task prompt (required)")
submitCmd.Flags().String("reply-to", "channel", "Reply destination (e.g. channel, telegram, stdout)")
submitCmd.Flags().Int("priority", 5, "Task priority (lower = higher priority)")
submitCmd.Flags().Bool("allow-writes", false, "Allow file writes")
submitCmd.Flags().Int("timeout", 300, "Timeout in seconds")
submitCmd.Flags().Bool("warm", false, "Route to warm worker for fast dispatch")

listCmd.Flags().Bool("all", false, "Include completed/failed tasks")

root.AddCommand(submitCmd, listCmd, statusCmd, cancelCmd, logsCmd, approveCmd, denyCmd)
```

- [ ] **Step 2: Update `daemon/cmd/gobrrr/gmail.go`**

Make the following edits:

1. Delete the entire `var (\n gmailListUnread bool ... )` block at the top of the file.
2. Rewrite each `RunE` to read its flags via `cmd.Flags().Get*`. Concretely:

   - **`gmailListCmd.RunE`** — read `unread`/`query`/`limit`/`account`:
     ```go
     RunE: func(cmd *cobra.Command, args []string) error {
         unread, _ := cmd.Flags().GetBool("unread")
         query, _ := cmd.Flags().GetString("query")
         limit, _ := cmd.Flags().GetInt("limit")
         account, _ := cmd.Flags().GetString("account")
         if unread && query == "" {
             query = "is:unread"
         } else if unread {
             query = "is:unread " + query
         }
         c := newClient()
         result, err := c.GmailList(query, limit, account, os.Getenv("GOBRRR_TASK_ID"))
         if err != nil {
             return err
         }
         fmt.Print(result)
         return nil
     },
     ```
   - **`gmailReadCmd.RunE`** — read `account`:
     ```go
     RunE: func(cmd *cobra.Command, args []string) error {
         account, _ := cmd.Flags().GetString("account")
         c := newClient()
         result, err := c.GmailRead(args[0], account, os.Getenv("GOBRRR_TASK_ID"))
         if err != nil {
             return err
         }
         fmt.Print(result)
         return nil
     },
     ```
   - **`gmailSendCmd.RunE`** — read `to`/`subject`/`body`/`account`:
     ```go
     RunE: func(cmd *cobra.Command, args []string) error {
         to, _ := cmd.Flags().GetString("to")
         subject, _ := cmd.Flags().GetString("subject")
         body, _ := cmd.Flags().GetString("body")
         account, _ := cmd.Flags().GetString("account")
         if to == "" {
             return fmt.Errorf("--to is required")
         }
         c := newClient()
         if err := c.GmailSend(to, subject, body, account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
             return err
         }
         fmt.Println("Message sent.")
         return nil
     },
     ```
   - **`gmailReplyCmd.RunE`** — read `body`/`account`:
     ```go
     RunE: func(cmd *cobra.Command, args []string) error {
         body, _ := cmd.Flags().GetString("body")
         account, _ := cmd.Flags().GetString("account")
         if body == "" {
             return fmt.Errorf("--body is required")
         }
         c := newClient()
         if err := c.GmailReply(args[0], body, account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
             return err
         }
         fmt.Println("Reply sent.")
         return nil
     },
     ```

3. Replace the flag-binding block at the bottom of `registerGmail`:

```go
gmailListCmd.Flags().Bool("unread", false, "Filter to unread messages")
gmailListCmd.Flags().String("query", "", "Gmail search query")
gmailListCmd.Flags().Int("limit", 10, "Maximum number of messages to return")
gmailListCmd.Flags().String("account", "default", "Account name")

gmailReadCmd.Flags().String("account", "default", "Account name")

gmailSendCmd.Flags().String("to", "", "Recipient email address (required)")
gmailSendCmd.Flags().String("subject", "", "Email subject")
gmailSendCmd.Flags().String("body", "", "Email body")
gmailSendCmd.Flags().String("account", "default", "Account name")

gmailReplyCmd.Flags().String("body", "", "Reply body (required)")
gmailReplyCmd.Flags().String("account", "default", "Account name")

gmailCmd.AddCommand(gmailListCmd, gmailReadCmd, gmailSendCmd, gmailReplyCmd)
root.AddCommand(gmailCmd)
```

- [ ] **Step 3: Update `daemon/cmd/gobrrr/gcal.go`**

Make the following edits:

1. Delete the entire `var (\n gcalTodayAccount string ... )` block.
2. Rewrite each `RunE` to read its flags via `cmd.Flags().GetString("account")` etc. Pattern: a single `account, _ := cmd.Flags().GetString("account")` line for the simple verbs (`today`, `week`, `get`, `delete`); the `create` and `update` `RunE`s read all of their flags explicitly. For example, `gcalCreateCmd.RunE`:

   ```go
   RunE: func(cmd *cobra.Command, args []string) error {
       title, _ := cmd.Flags().GetString("title")
       start, _ := cmd.Flags().GetString("start")
       end, _ := cmd.Flags().GetString("end")
       description, _ := cmd.Flags().GetString("description")
       account, _ := cmd.Flags().GetString("account")
       if title == "" {
           return fmt.Errorf("--title is required")
       }
       c := newClient()
       if err := c.GcalCreateEvent(title, start, end, description, account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
           return err
       }
       fmt.Println("Event created.")
       return nil
   },
   ```

   And `gcalUpdateCmd.RunE`:

   ```go
   RunE: func(cmd *cobra.Command, args []string) error {
       title, _ := cmd.Flags().GetString("title")
       start, _ := cmd.Flags().GetString("start")
       end, _ := cmd.Flags().GetString("end")
       account, _ := cmd.Flags().GetString("account")
       c := newClient()
       if err := c.GcalUpdateEvent(args[0], title, start, end, account, os.Getenv("GOBRRR_TASK_ID")); err != nil {
           return err
       }
       fmt.Println("Event updated.")
       return nil
   },
   ```

3. Replace the flag-binding block at the bottom of `registerGcal`:

```go
gcalTodayCmd.Flags().String("account", "default", "Account name")
gcalWeekCmd.Flags().String("account", "default", "Account name")
gcalGetCmd.Flags().String("account", "default", "Account name")

gcalCreateCmd.Flags().String("title", "", "Event title (required)")
gcalCreateCmd.Flags().String("start", "", "Event start time (RFC3339)")
gcalCreateCmd.Flags().String("end", "", "Event end time (RFC3339)")
gcalCreateCmd.Flags().String("description", "", "Event description")
gcalCreateCmd.Flags().String("account", "default", "Account name")

gcalUpdateCmd.Flags().String("title", "", "New event title")
gcalUpdateCmd.Flags().String("start", "", "New event start time (RFC3339)")
gcalUpdateCmd.Flags().String("end", "", "New event end time (RFC3339)")
gcalUpdateCmd.Flags().String("account", "default", "Account name")

gcalDeleteCmd.Flags().String("account", "default", "Account name")

gcalCmd.AddCommand(gcalTodayCmd, gcalWeekCmd, gcalGetCmd, gcalCreateCmd, gcalUpdateCmd, gcalDeleteCmd)
root.AddCommand(gcalCmd)
```

- [ ] **Step 4: Verify the build, vet, and tests**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./...
go vet ./...
go test ./...
```
Expected: all clean.

### Task 6: Standardize flags in `memory`, `setup`

**Files:**
- Modify: `daemon/cmd/gobrrr/memory.go`
- Modify: `daemon/cmd/gobrrr/setup.go`

**Goal:** Same standardization in the remaining two files. Skill, session, and timer already use the target style — they need no changes.

- [ ] **Step 1: Update `daemon/cmd/gobrrr/memory.go`**

Make the following edits:

1. Delete the entire `var (\n memorySaveContent string ... )` block at the top.
2. Rewrite each `RunE` to read its flags via `cmd.Flags().Get*`:

   - **`memorySaveCmd.RunE`** reads `content`/`tags`/`source`:
     ```go
     RunE: func(cmd *cobra.Command, args []string) error {
         content, _ := cmd.Flags().GetString("content")
         tagsStr, _ := cmd.Flags().GetString("tags")
         source, _ := cmd.Flags().GetString("source")
         if content == "" {
             return fmt.Errorf("--content is required")
         }
         var tags []string
         if tagsStr != "" {
             for _, t := range strings.Split(tagsStr, ",") {
                 t = strings.TrimSpace(t)
                 if t != "" {
                     tags = append(tags, t)
                 }
             }
         }
         c := newClient()
         entry, err := c.SaveMemory(content, tags, source)
         if err != nil {
             return err
         }
         printMemoryEntry(entry)
         return nil
     },
     ```
   - **`memorySearchCmd.RunE`** reads `tags`:
     ```go
     RunE: func(cmd *cobra.Command, args []string) error {
         query := ""
         if len(args) > 0 {
             query = args[0]
         }
         tagsStr, _ := cmd.Flags().GetString("tags")
         var tags []string
         if tagsStr != "" {
             for _, t := range strings.Split(tagsStr, ",") {
                 t = strings.TrimSpace(t)
                 if t != "" {
                     tags = append(tags, t)
                 }
             }
         }
         c := newClient()
         entries, err := c.SearchMemory(query, tags, 0)
         if err != nil {
             return err
         }
         printMemoryList(entries)
         return nil
     },
     ```
   - **`memoryListCmd.RunE`** reads `limit`:
     ```go
     RunE: func(cmd *cobra.Command, args []string) error {
         limit, _ := cmd.Flags().GetInt("limit")
         c := newClient()
         entries, err := c.SearchMemory("", nil, limit)
         if err != nil {
             return err
         }
         printMemoryList(entries)
         return nil
     },
     ```

3. Replace the flag-binding block:

```go
memorySaveCmd.Flags().String("content", "", "Memory content (required)")
memorySaveCmd.Flags().String("tags", "", "Comma-separated tags")
memorySaveCmd.Flags().String("source", "", "Source of the memory")

memorySearchCmd.Flags().String("tags", "", "Comma-separated tags to filter by")

memoryListCmd.Flags().Int("limit", 20, "Maximum number of entries to return")

memoryCmd.AddCommand(memorySaveCmd, memorySearchCmd, memoryListCmd, memoryGetCmd, memoryDeleteCmd)
root.AddCommand(memoryCmd)
```

- [ ] **Step 2: Update `daemon/cmd/gobrrr/setup.go`**

Make the following edit. The existing `setup.go` from Task 2 already keeps `setupGoogleAccountName` as a closure-local var bound via `StringVar`. Replace it with the no-`Var` style so it stays consistent with every other verb post-refactor.

In `registerSetup`, replace this block:

```go
var setupGoogleAccountName string

setupGoogleAccountCmd := &cobra.Command{
    Use:   "google-account",
    Short: "Add a Google account",
    RunE: func(cmd *cobra.Command, args []string) error {
        return setup.RunGoogleAccountSetup(setupGoogleAccountName)
    },
}

setupGoogleAccountCmd.Flags().StringVar(&setupGoogleAccountName, "name", "", "Account label (e.g. personal, work)")
```

with:

```go
setupGoogleAccountCmd := &cobra.Command{
    Use:   "google-account",
    Short: "Add a Google account",
    RunE: func(cmd *cobra.Command, args []string) error {
        name, _ := cmd.Flags().GetString("name")
        return setup.RunGoogleAccountSetup(name)
    },
}

setupGoogleAccountCmd.Flags().String("name", "", "Account label (e.g. personal, work)")
```

- [ ] **Step 3: Verify the build, vet, and tests**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build ./...
go vet ./...
go test ./...
```
Expected: all clean.

### Task 7: Verify zero `*Var` flag bindings, diff `--help`, commit B

**Files:** None modified — verification + commit only.

**Goal:** Prove the standardization is complete (zero `Flags().StringVar`/`BoolVar`/`IntVar` calls in `cmd/gobrrr`), prove the CLI surface still matches the baseline, then commit.

- [ ] **Step 1: Grep for any remaining `*Var` flag bindings**

Run:
```bash
cd /home/racterub/github/gobrrr
grep -rnE 'Flags\(\)\.[A-Z][a-zA-Z]*Var\(' daemon/cmd/gobrrr/
```
Expected: no output. If any line is printed, that file still has a package-global flag var — fix it before continuing.

- [ ] **Step 2: Grep for package-level flag-value vars**

Run:
```bash
cd /home/racterub/github/gobrrr
grep -nE '^var\s+[a-z][a-zA-Z]+\s+(string|bool|int|int64|float64)\s*$' daemon/cmd/gobrrr/*.go
grep -nE '^\s+[a-z][a-zA-Z]+\s+(string|bool|int|int64|float64)\s*$' daemon/cmd/gobrrr/*.go
```
Expected: no output for both greps. If anything appears, it's a leftover flag var (or a type field inside a struct literal — which the grep shouldn't catch but verify). Investigate before continuing.

- [ ] **Step 3: Build the post-standardization binary and diff `--help`**

Run:
```bash
cd /home/racterub/github/gobrrr/daemon
CGO_ENABLED=0 go build -o /tmp/gobrrr-final ./cmd/gobrrr/
mkdir -p /tmp/gobrrr-help-final
B=/tmp/gobrrr-final
H=/tmp/gobrrr-help-final
$B --help                          > $H/root.txt
$B daemon --help                   > $H/daemon.txt
$B daemon start --help             > $H/daemon-start.txt
$B daemon status --help            > $H/daemon-status.txt
$B submit --help                   > $H/submit.txt
$B list --help                     > $H/list.txt
$B status --help                   > $H/task-status.txt
$B cancel --help                   > $H/cancel.txt
$B logs --help                     > $H/logs.txt
$B approve --help                  > $H/approve.txt
$B deny --help                     > $H/deny.txt
$B gmail --help                    > $H/gmail.txt
$B gmail list --help               > $H/gmail-list.txt
$B gmail read --help               > $H/gmail-read.txt
$B gmail send --help               > $H/gmail-send.txt
$B gmail reply --help              > $H/gmail-reply.txt
$B gcal --help                     > $H/gcal.txt
$B gcal today --help               > $H/gcal-today.txt
$B gcal week --help                > $H/gcal-week.txt
$B gcal get --help                 > $H/gcal-get.txt
$B gcal create --help              > $H/gcal-create.txt
$B gcal update --help              > $H/gcal-update.txt
$B gcal delete --help              > $H/gcal-delete.txt
$B memory --help                   > $H/memory.txt
$B memory save --help              > $H/memory-save.txt
$B memory search --help            > $H/memory-search.txt
$B memory list --help              > $H/memory-list.txt
$B memory get --help               > $H/memory-get.txt
$B memory delete --help            > $H/memory-delete.txt
$B session --help                  > $H/session.txt
$B session status --help           > $H/session-status.txt
$B session start --help            > $H/session-start.txt
$B session stop --help             > $H/session-stop.txt
$B session restart --help          > $H/session-restart.txt
$B timer --help                    > $H/timer.txt
$B timer create --help             > $H/timer-create.txt
$B timer list --help               > $H/timer-list.txt
$B timer remove --help             > $H/timer-remove.txt
$B skill --help                    > $H/skill.txt
$B skill list --help               > $H/skill-list.txt
$B skill search --help             > $H/skill-search.txt
$B skill install --help            > $H/skill-install.txt
$B skill approve --help            > $H/skill-approve.txt
$B skill deny --help               > $H/skill-deny.txt
$B skill uninstall --help          > $H/skill-uninstall.txt
$B setup --help                    > $H/setup.txt
$B setup google-account --help     > $H/setup-google-account.txt
diff -ru /tmp/gobrrr-help-baseline /tmp/gobrrr-help-final
```
Expected: `diff` exits 0 with no output. Standardization must not have changed any default value, flag name, or `--help` line. If anything differs, fix the offending file before committing.

- [ ] **Step 4: Commit B**

Run:
```bash
cd /home/racterub/github/gobrrr
git add daemon/cmd/gobrrr/task.go daemon/cmd/gobrrr/gmail.go daemon/cmd/gobrrr/gcal.go daemon/cmd/gobrrr/memory.go daemon/cmd/gobrrr/setup.go
git status --short
git diff --cached --stat
```
Expected: 5 modified files, no new files, no deletions of files.

Then create the commit:

```bash
git commit -m "$(cat <<'EOF'
refactor(cli): standardize cobra flag wiring

Replace every Flags().StringVar/BoolVar/IntVar(&pkgVar, ...) call
in cmd/gobrrr with the no-Var equivalent (String/Bool/Int) and read
flag values inside RunE via cmd.Flags().GetString("name") (and
GetBool/GetInt). Delete the package-level flag-value vars that were
backing the StringVar/BoolVar/IntVar bindings.

The timer verb already used this style; this commit makes every
other verb consistent with it. Net effect: zero package-level flag-
value vars in cmd/gobrrr (verified by grep). Every --help output is
byte-identical to the prior master.

Structural change.

AI-Ratio: 1.0
Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
git log --oneline -2
```
Expected: two commits on `refactor/10-split-main` — the per-verb split, then the flag-style standardization.

- [ ] **Step 5: Report completion**

Print a short summary:

```
Refactor #10 implementation complete on branch refactor/10-split-main.
Two commits:
  - refactor(cli): split main.go into per-verb files
  - refactor(cli): standardize cobra flag wiring

main.go: 1064 → ~50 lines.
Package-level flag vars: 37 → 0.
--help output: byte-identical to master.

Next step: hand to the user for review (whole-branch code review +
deploy + merge), per the pattern from refactors #7, #9, #8.
```

---

## Acceptance criteria (from spec)

- [ ] `main.go` ≤ 100 lines (target ~50). Verified by `wc -l`.
- [ ] Each verb has its own file with a `register<Verb>(root)` function.
- [ ] Zero package-level flag-value vars remain (verified by grep in Task 7 Steps 1–2).
- [ ] CLI smoke tests still pass; all `--help` output is unchanged (verified by `diff` in Tasks 4 and 7).
- [ ] Two commits, one per phase, both labeled `Structural change.` per the global CLAUDE.md tidy-first rule.
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all clean at every commit.

## Out of scope (per spec)

- Typed response structs replacing `map[string]any` (Refactor #12-derivative; tracked elsewhere).
- Exit-code policy reconciliation (M5 in original review; punted).
- Any change to flag names, defaults, or help text.
