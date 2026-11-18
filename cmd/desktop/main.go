package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/claude"
	"github.com/shenming0821/Sasa/internal/codex"
	"github.com/shenming0821/Sasa/internal/release"
	process "github.com/shenming0821/Sasa/internal/runtime"
	"github.com/shenming0821/Sasa/internal/storage"
	"github.com/shenming0821/Sasa/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func migrateDataDir(legacy, target string) error {
	if _, err := os.Stat(target); err == nil {
		entries, err := os.ReadDir(target)
		if err != nil {
			return err
		}
		if len(entries) > 0 && (len(entries) > 1 || entries[0].Name() != "release-channel") {
			return fmt.Errorf("目标数据目录已存在且非空：%s", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	if err := os.Rename(legacy, target); err != nil {
		return err
	}
	for _, name := range []string{"sasa.db", "sasa.db-wal", "sasa.db-shm", "sasa.lock", "sasa.sqlite"} {
		old := filepath.Join(target, name)
		newPath := filepath.Join(target, "sasa"+name[len("sasa"):])
		if _, err := os.Stat(old); err == nil {
			if err := os.Rename(old, newPath); err != nil {
				return fmt.Errorf("重命名数据库文件：%w", err)
			}
		}
	}
	dbPath := filepath.Join(target, "sasa.db")
	if _, err := os.Stat(dbPath); err == nil {
		if err := rewriteRuntimePaths(dbPath, legacy, target); err != nil {
			return fmt.Errorf("重写数据库路径：%w", err)
		}
	}
	return nil
}

func rewriteRuntimePaths(dbPath, legacy, target string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	legacyRuntime := filepath.Join(legacy, "runtimes")
	targetRuntime := filepath.Join(target, "runtimes")
	_, err = db.Exec("UPDATE runtime_identity SET home = REPLACE(home, ?, ?)", legacyRuntime, targetRuntime)
	return err
}

func run() error {
	cwd := flag.String("cwd", "", "Initial working directory")
	executable := flag.String("codex", "codex", "Codex executable")
	dataDir := flag.String("data-dir", "", "Application data directory (default: user config directory/channel name)")
	showVersion := flag.Bool("version", false, "Print release identity as JSON and exit")
	readyFile := flag.String("ready-file", "", "Write startup acknowledgement after restoring the window")
	launchToken := flag.String("launch-token", "", "Startup acknowledgement token")
	flag.Parse()
	identity, err := release.Current()
	if err != nil {
		return err
	}
	if *showVersion {
		return json.NewEncoder(os.Stdout).Encode(identity)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	legacyName := "Sasa"
	if identity.Channel == "preview" {
		legacyName = "Sasa Preview"
	}
	legacy := filepath.Join(configDir, legacyName)
	target := filepath.Join(configDir, identity.DataName())
	if *dataDir == "" {
		*dataDir = target
	}
	resolved, _ := filepath.Abs(*dataDir)
	if resolved == legacy && legacy != target {
		*dataDir = target
	}
	if legacy != target {
		if _, err := os.Stat(legacy); err == nil && *dataDir == target {
			if migrateErr := migrateDataDir(legacy, target); migrateErr != nil {
				return fmt.Errorf("迁移旧数据目录：%w", migrateErr)
			}
		}
	}
	*dataDir, err = filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if err := identity.ValidateDataDir(*dataDir); err != nil {
		return err
	}
	store, err := storage.Open(*dataDir)
	if err != nil {
		return fmt.Errorf("open application index: %w", err)
	}
	defer store.Close()
	if err := identity.BindDataDir(*dataDir); err != nil {
		return err
	}
	if err := identity.PrepareRuntime(*dataDir); err != nil {
		return err
	}
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
	desktop.SetRelease(identity, func() error { return identity.WriteReady(*readyFile, *launchToken, *dataDir) })
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
