package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/go-telegram/bot/models"

	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/access"
	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/bot"
	"github.com/racterub/gobrrr/cmd/gobrrr-telegram/mcpserver"
	"github.com/racterub/gobrrr/internal/client"
	"github.com/racterub/gobrrr/internal/config"
)

func main() {
	stateDir := os.Getenv("TELEGRAM_STATE_DIR")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".claude", "channels", "telegram")
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		fail("mkdir state dir: %v", err)
	}
	loadDotEnv(filepath.Join(stateDir, ".env"))

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		fmt.Fprintf(os.Stderr,
			"gobrrr-telegram: TELEGRAM_BOT_TOKEN required\n"+
				"  set in %s/.env\n"+
				"  format: TELEGRAM_BOT_TOKEN=123456789:AAH...\n",
			stateDir)
		os.Exit(1)
	}
	static := os.Getenv("TELEGRAM_ACCESS_MODE") == "static"

	store := access.NewStore(stateDir, static)
	if _, err := store.Load(); err != nil {
		fail("load access: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var mcpSrv *mcpserver.Server
	b, err := bot.New(token, stateDir, store, func(ctx context.Context, upd *models.Update, attachPath, attachFileID string) {
		// late binding: mcpSrv is set below
		if mcpSrv != nil {
			mcpSrv.EmitInbound(ctx, upd, attachPath, attachFileID)
		}
	})
	if err != nil {
		fail("bot init: %v", err)
	}
	mcpSrv = mcpserver.New(b, store, stateDir)
	b.SetOnPermissionReply(mcpSrv.SendPermissionDecision)

	// Connect to the gobrrr daemon for approval routing. Defaults match the
	// daemon's default socket path; override with GOBRRR_SOCKET_PATH.
	sockPath := os.Getenv("GOBRRR_SOCKET_PATH")
	if sockPath == "" {
		sockPath = filepath.Join(config.GobrrDir(), "gobrrr.sock")
	}
	daemonClient := client.New(sockPath)
	sub := bot.NewApprovalSubscriber(b, daemonClient)
	b.SetOnApprovalCallback(sub.HandleApprovalCallback)

	go func() {
		defer recoverAndLog("approval subscriber")
		if err := sub.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "gobrrr-telegram: approval subscriber stopped: %v\n", err)
		}
	}()

	// Bot long-poll in a goroutine; MCP stdio server blocks main.
	go func() {
		defer recoverAndLog("bot loop")
		if err := b.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "gobrrr-telegram: bot stopped: %v\n", err)
		}
	}()

	if err := mcpSrv.ServeStdio(ctx); err != nil {
		fail("mcp stdio: %v", err)
	}
}

func loadDotEnv(path string) {
	_ = os.Chmod(path, 0600)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "="); i > 0 {
			key := strings.TrimSpace(line[:i])
			val := strings.TrimSpace(line[i+1:])
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
}

func recoverAndLog(where string) {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "gobrrr-telegram: panic in %s: %v\n", where, r)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gobrrr-telegram: "+format+"\n", args...)
	os.Exit(1)
}
