package release

import (
	"fmt"
	"os"
	"path/filepath"
)

func (info Info) PrepareRuntime(dataDir string) error {
	if info.Channel != "preview" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, runtime := range []struct {
		name, variable, source string
		files                  []string
	}{
		{"codex", "CODEX_HOME", filepath.Join(home, ".codex"), []string{"config.toml", "auth.json"}},
		{"claude", "CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"), []string{"settings.json", ".credentials.json"}},
	} {
		source := runtime.source
		if configured := os.Getenv(runtime.variable); configured != "" {
			source = configured
		}
		destination := filepath.Join(dataDir, "runtimes", runtime.name)
		if info, err := os.Lstat(filepath.Dir(destination)); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Preview runtimes directory cannot be a symbolic link")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := seedRuntime(source, destination, runtime.files); err != nil {
			return fmt.Errorf("prepare Preview %s: %w", runtime.name, err)
		}
		if err := os.Setenv(runtime.variable, destination); err != nil {
			return err
		}
	}
	return nil
}

func seedRuntime(source, destination string, names []string) error {
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime home is not a directory: %s", destination)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(destination), ".runtime-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(source, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(staging, name), raw, 0600); err != nil {
			return err
		}
	}
	return os.Rename(staging, destination)
}
