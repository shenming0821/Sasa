package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shenming0821/Fuyu/internal/codex"
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
	ctx, cancel := codex.RequestContext()
	defer cancel()
	version, versionErr := codex.Version(ctx, *executable)
	if versionErr != nil {
		version = "Codex 不可用"
	}
	serverDir, err := os.UserHomeDir()
	if err != nil {
		serverDir = os.TempDir()
	}
	client, err := codex.Start(ctx, *executable, serverDir)
	var backend ui.Backend
	if client != nil {
		defer client.Close()
		backend = client
	}
	desktop := ui.New(backend, strings.TrimSpace(version), *cwd, err)
	if err := desktop.AttachStore(store); err != nil {
		return fmt.Errorf("restore application index (original data preserved): %w", err)
	}
	return desktop.Run()
}
