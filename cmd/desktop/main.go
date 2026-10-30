package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/claude"
	"github.com/shenming0821/Fuyu/internal/codex"
	process "github.com/shenming0821/Fuyu/internal/runtime"
	"github.com/shenming0821/Fuyu/internal/storage"
	"github.com/shenming0821/Fuyu/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cwd := flag.String("cwd", "", "Initial working directory")
	executable := flag.String("codex", "codex", "Codex executable")
	dataDir := flag.String("data-dir", "", "Application data directory (default: user config directory/Fuyu)")
	flag.Parse()
	if *dataDir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		*dataDir = filepath.Join(configDir, "Fuyu")
	}
	store, err := storage.Open(*dataDir)
	if err != nil {
		return fmt.Errorf("open application index: %w", err)
	}
	defer store.Close()
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		codexHome = filepath.Join(home, ".codex")
	}
	codexHome, err = filepath.Abs(codexHome)
	if err != nil {
		return err
	}
	if canonical, err := filepath.EvalSymlinks(codexHome); err == nil {
		codexHome = canonical
	}
	if err := store.BindRuntime(codexHome); err != nil {
		return err
	}
	if *cwd != "" {
		path, err := filepath.Abs(*cwd)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("working directory is unavailable: %s", path)
		}
		*cwd = path
	}
	desktop := ui.New(nil, "Codex", *cwd, nil)
	desktop.SetStartup(func(ctx context.Context, publish func(ui.RuntimeConnection)) {
		loadRuntimes(ctx, *executable, *dataDir, publish)
	})
	if err := desktop.AttachStore(store); err != nil {
		return fmt.Errorf("restore application index (original data preserved): %w", err)
	}
	return desktop.Run()
}

func loadRuntimes(ctx context.Context, executable, dataDir string, publish func(ui.RuntimeConnection)) {
	resolved, searchPath, launchErr := process.ResolveCodex(executable)
	if launchErr == nil {
		launchErr = os.Setenv("PATH", searchPath)
	}
	if ctx.Err() != nil {
		return
	}
	var workers sync.WaitGroup
	defer workers.Wait()
	workers.Go(func() {
		path, err := claude.ResolveExecutable()
		var client *claude.Client
		if err == nil {
			client, err = claude.New(path, "", filepath.Join(dataDir, "claude"))
		}
		result := ui.RuntimeConnection{Kind: agent.Claude, Err: err}
		if client != nil {
			defer client.Close()
			result.Backend, result.Runtime = client, client.Home()
		}
		publish(result)
		<-ctx.Done()
	})
	if launchErr != nil {
		publish(ui.RuntimeConnection{Kind: agent.Codex, Err: launchErr})
		return
	}
	workers.Go(func() {
		versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if version, err := codex.Version(versionCtx, resolved); err == nil {
			publish(ui.RuntimeConnection{Version: strings.TrimSpace(version)})
		}
	})
	serverDir, err := os.UserHomeDir()
	if err != nil {
		serverDir = os.TempDir()
	}
	startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	client, err := codex.Start(startCtx, resolved, serverDir)
	cancel()
	result := ui.RuntimeConnection{Kind: agent.Codex, Err: err}
	if client != nil {
		defer client.Close()
		result.Backend = client
	}
	publish(result)
	<-ctx.Done()
}
