package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameNeverReplacesExistingTarget(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	for path, text := range map[string]string{source: "source", target: "keep"} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Rename(source, target); err == nil {
		t.Fatal("replaced existing target")
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "keep" {
		t.Fatal("target changed", err)
	}
}
