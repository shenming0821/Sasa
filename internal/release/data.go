package release

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (info Info) ValidateDataDir(directory string) error {
	marker, err := os.ReadFile(filepath.Join(directory, "release-channel"))
	if err == nil {
		if strings.TrimSpace(string(marker)) != info.Channel {
			return fmt.Errorf("data directory belongs to another release channel: %s", directory)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if info.Channel != "preview" {
		return nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	stable, err := canonicalPath(filepath.Join(config, "Sasa"))
	if err != nil {
		return err
	}
	directory, err = canonicalPath(directory)
	if err != nil {
		return err
	}
	if relative, err := filepath.Rel(stable, directory); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("Preview requires a separate data directory: %s", directory)
	}
	if _, err := os.Stat(filepath.Join(directory, "sasa.db")); err == nil {
		return fmt.Errorf("Preview cannot adopt an unmarked existing database: %s", directory)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func canonicalPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent, err := canonicalPath(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func (info Info) BindDataDir(directory string) error {
	path := filepath.Join(directory, "release-channel")
	if _, err := os.Stat(path); err == nil {
		return info.ValidateDataDir(directory)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(info.Channel+"\n"), 0600)
}
