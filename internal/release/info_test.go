package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleIdentityAndPromotion(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "Fuyu.app", "Contents", "MacOS", "Fuyu")
	resources := filepath.Join(root, "Fuyu.app", "Contents", "Resources")
	if err := os.MkdirAll(resources, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readInfo(executable); err == nil {
		t.Fatal("bundled executable accepted missing identity")
	}
	for _, channel := range []string{"preview", "stable"} {
		info := Info{Channel: channel, BuildID: BuildID, SourceVersion: SourceVersion, SourceDigest: SourceDigest, Version: "0.1.0", SchemaVersion: 6}
		raw, err := json.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(resources, "release.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		got, err := readInfo(executable)
		if err != nil || got != info {
			t.Fatalf("channel %s: %+v, %v", channel, got, err)
		}
	}
	if err := os.WriteFile(filepath.Join(resources, "release.json"), []byte(`{"channel":"other"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readInfo(executable); err == nil {
		t.Fatal("accepted invalid channel")
	}
	if err := os.WriteFile(filepath.Join(resources, "release.json"), []byte(`{"channel":"preview","buildID":"another-build"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readInfo(executable); err == nil {
		t.Fatal("accepted another executable's identity")
	}
}

func TestPreviewSeedsOnlyConfigOnceAndDoesNotShareHistory(t *testing.T) {
	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "codex")
	for name, text := range map[string]string{"config.toml": "config-v1", "auth.json": "credential", "history.jsonl": "private-history"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{"config.toml", "auth.json"}
	if err := seedRuntime(source, destination, files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "history.jsonl")); !os.IsNotExist(err) {
		t.Fatal("history was copied")
	}
	if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte("config-v2"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := seedRuntime(source, destination, files); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "config.toml"))
	if err != nil || string(got) != "config-v1" {
		t.Fatalf("existing Preview config was overwritten: %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(destination, "auth.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credential permissions: %v, %v", info, err)
	}
	alias := filepath.Join(t.TempDir(), "codex")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	if err := seedRuntime(source, alias, files); err == nil {
		t.Fatal("accepted a shared runtime symlink")
	}
}

func TestChannelsCannotAdoptEachOthersData(t *testing.T) {
	directory := t.TempDir()
	preview, stable := Info{Channel: "preview"}, Info{Channel: "stable"}
	if err := preview.ValidateDataDir(directory); err != nil {
		t.Fatal(err)
	}
	if err := preview.BindDataDir(directory); err != nil {
		t.Fatal(err)
	}
	if err := stable.ValidateDataDir(directory); err == nil {
		t.Fatal("stable accepted Preview data")
	}
	other := t.TempDir()
	if err := stable.BindDataDir(other); err != nil {
		t.Fatal(err)
	}
	if err := preview.ValidateDataDir(other); err == nil {
		t.Fatal("Preview accepted stable data")
	}
	legacy := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacy, "fuyu.db"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := preview.ValidateDataDir(legacy); err == nil {
		t.Fatal("Preview accepted unmarked data")
	}
	if err := stable.ValidateDataDir(legacy); err != nil {
		t.Fatal(err)
	}
}

func TestReadyAcknowledgement(t *testing.T) {
	info := Info{Channel: "preview", BuildID: "candidate"}
	path := filepath.Join(t.TempDir(), "ready.json")
	if err := info.WriteReady(path, "", "/isolated"); err == nil {
		t.Fatal("missing token accepted")
	}
	if err := info.WriteReady(path, "nonce", "/isolated"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ready Ready
	if err := json.Unmarshal(raw, &ready); err != nil || ready.Token != "nonce" || ready.PID != os.Getpid() || ready.DataDir != "/isolated" || ready.BuildID != "candidate" {
		t.Fatalf("acknowledgement: %+v, %v", ready, err)
	}
	if !strings.Contains(info.WindowTitle(), "Preview") {
		t.Fatal("Preview window lacks channel identity")
	}
}
