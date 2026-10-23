package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCodexDiscoveryAndOverrides(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	inherited := writeExecutable(t, filepath.Join(home, "inherited/fuyu-test-codex"), "#!/bin/sh\nexit 0\n")
	login := writeExecutable(t, filepath.Join(home, "login/fuyu-test-codex"), "#!/bin/sh\nexit 0\n")
	fallback := writeExecutable(t, filepath.Join(home, ".bun/bin/fuyu-test-codex"), "#!/bin/sh\nexit 0\n")
	for _, tc := range []struct {
		name, executable, inherited, login, want string
	}{
		{"inherited first", "fuyu-test-codex", filepath.Dir(inherited), filepath.Dir(login), inherited},
		{"login next", "fuyu-test-codex", "/usr/bin:/bin", filepath.Dir(login), login},
		{"common installation", "fuyu-test-codex", "/usr/bin:/bin", "", fallback},
		{"absolute override", fallback, filepath.Dir(inherited), filepath.Dir(login), fallback},
		{"invalid override", filepath.Join(home, "missing/codex"), filepath.Dir(inherited), "", ""},
		{"missing command", "fuyu-uninstalled-test-command", "/usr/bin:/bin", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, path, err := resolveCodex(tc.executable, tc.inherited, tc.login, home, "")
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
			if !strings.Contains(path, "/usr/bin") {
				t.Fatalf("system tools missing: %s", path)
			}
		})
	}
}

func TestFinderPathStartsNodeWrapper(t *testing.T) {
	home := t.TempDir()
	wrapper := writeExecutable(t, filepath.Join(home, ".bun/bin/fuyu-test-codex"), "#!/usr/bin/env node\nignored by fake node\n")
	writeExecutable(t, filepath.Join(home, ".local/share/nvm/v24.9.0/bin/node"), "#!/bin/sh\nprintf wrong-version\n")
	writeExecutable(t, filepath.Join(home, ".local/share/nvm/v24.10.0/bin/node"), "#!/bin/sh\nprintf finder-launch-ok\n")
	resolved, path, err := resolveCodex("fuyu-test-codex", "/usr/bin:/bin:/usr/sbin:/sbin", filepath.Join(home, ".local/share/nvm/v24.10.0/bin"), home, "")
	if err != nil || resolved != wrapper {
		t.Fatalf("resolve: %q, %v", resolved, err)
	}
	// This is the same PATH installation performed before Version and Start.
	cmd := exec.Command(resolved, "--version")
	cmd.Env = []string{"PATH=" + path}
	output, err := cmd.Output()
	if err != nil || string(output) != "finder-launch-ok" {
		t.Fatalf("wrapper launch: %q, %v", output, err)
	}
}

func TestDiscoveryRejectsNonExecutableAndRelativeSearch(t *testing.T) {
	home := t.TempDir()
	path := writeExecutable(t, filepath.Join(home, ".bun/bin/fuyu-test-codex"), "#!/bin/sh\n")
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	_, search, err := resolveCodex("fuyu-test-codex", ".:relative/bin::/usr/bin", "/usr/bin:/bin", home, "")
	if err == nil {
		t.Fatal("non-executable file accepted")
	}
	seen := make(map[string]bool)
	for _, dir := range filepath.SplitList(search) {
		if !filepath.IsAbs(dir) || seen[dir] {
			t.Fatalf("unsafe or duplicate directory: %q", dir)
		}
		seen[dir] = true
	}
}

func TestLoginShellPathIsBoundedAndParsesOnlyMarkedOutput(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"startup noise", "printf 'startup noise\\n\\000FUYU_PATH\\000/isolated/bin:/usr/bin\\n'", "/isolated/bin:/usr/bin"},
		{"missing marker", "printf '/untrusted-output'", ""},
		{"failure", "exit 1", ""},
		{"timeout", "exec /bin/sleep 30", ""},
		{"output limit", "/usr/bin/head -c 70000 /dev/zero", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shell := writeExecutable(t, filepath.Join(t.TempDir(), "shell"), "#!/bin/sh\n"+tc.body+"\n")
			if got := loginShellPath(shell); got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestNodeRequirementAndVersionOrder(t *testing.T) {
	home := t.TempDir()
	wrapper := writeExecutable(t, filepath.Join(home, "tools/codex"), "#!/usr/bin/env node\n")
	if _, _, err := checkedCodex(wrapper, filepath.Join(home, "empty")); err == nil || !strings.Contains(err.Error(), "Node.js") {
		t.Fatalf("missing Node.js was not explained: %v", err)
	}
	for _, version := range []string{"v9.0.0", "v24.9.0", "v24.10.0"} {
		writeExecutable(t, filepath.Join(home, ".nvm/versions/node", version, "bin/node"), "#!/bin/sh\n")
	}
	_, path, _ := resolveCodex("fuyu-uninstalled-test-command", "/usr/bin:/bin", "", home, "")
	if !(strings.Index(path, "v24.10.0") < strings.Index(path, "v24.9.0") && strings.Index(path, "v24.9.0") < strings.Index(path, "v9.0.0")) {
		t.Fatalf("wrong version priority: %s", path)
	}
}
