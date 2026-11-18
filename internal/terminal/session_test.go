package terminal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartValidatesWorkspaceWithoutStartingShell(t *testing.T) {
	root := t.TempDir()
	session, err := Start(root)
	if err != nil || session.root != root || session.handle != nil || session.token != 0 || session.Exited() {
		t.Fatal("terminal must defer native shell creation until mounted", session, err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"relative", filepath.Join(root, "missing"), file} {
		if _, err := Start(invalid); err == nil {
			t.Fatal("invalid terminal workspace accepted", invalid)
		}
	}
}

func TestLibraryPathsUseAppResources(t *testing.T) {
	library, resources := libraryPaths("/Applications/Sasa.app/Contents/MacOS/Sasa")
	if library != "/Applications/Sasa.app/Contents/Frameworks/libghostty.dylib" || resources != "/Applications/Sasa.app/Contents/Resources/ghostty" {
		t.Fatal(library, resources)
	}
	library, resources = libraryPaths("/tmp/Sasa")
	if library != "/tmp/libghostty.dylib" || resources != "/tmp/ghostty" {
		t.Fatal(library, resources)
	}
}

func TestLegacyTerminalPreferenceNormalizesToNativeGhostty(t *testing.T) {
	for _, value := range []string{"", "terminal", "xterm", Ghostty, "unknown"} {
		if Normalize(value) != Ghostty || Label(value) != "Ghostty" {
			t.Fatal("unsupported engine remained selectable", value)
		}
	}
	if len(Choices) != 1 || Choices[0].ID != Ghostty {
		t.Fatal("only the implemented native engine should be offered")
	}
}
