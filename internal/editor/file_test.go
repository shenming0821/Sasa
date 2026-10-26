package editor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSavePreservesBytesAndDetectsExternalChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "中文 file.txt")
	before := []byte("\xef\xbb\xbf第一行\r\n第二行\r\n")
	if e := os.WriteFile(path, before, 0750); e != nil {
		t.Fatal(e)
	}
	f, e := Read(path)
	if e != nil || !f.BOM || !f.CRLF || f.Reason != "" {
		t.Fatal(f, e)
	}
	saved, e := Save(context.Background(), f, "修改\n第二行\n", false)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "\xef\xbb\xbf修改\r\n第二行\r\n" {
		t.Fatalf("lost encoding or endings: %q", raw)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0750 {
		t.Fatal(info.Mode())
	}
	os.WriteFile(path, []byte("external change"), 0750)
	if _, e = Save(context.Background(), saved, "mine", false); !errors.Is(e, ErrConflict) {
		t.Fatal("overwrote external content", e)
	}
	raw, _ = os.ReadFile(path)
	if string(raw) != "external change" {
		t.Fatal("conflict wrote file")
	}
}
func TestReadRejectsSpecialAndLinksStayReadonly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain")
	os.WriteFile(path, []byte("hello"), 0600)
	os.Symlink(path, filepath.Join(dir, "link"))
	f, e := Read(filepath.Join(dir, "link"))
	if e != nil || f.Reason == "" {
		t.Fatal("link writable", e)
	}
	if _, e = Save(context.Background(), f, "bad", false); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	os.WriteFile(path, []byte("a\r\nb\nc"), 0600)
	f, e = Read(path)
	if e != nil || f.Reason == "" {
		t.Fatal("mixed endings editable")
	}
}
func TestRecoverySurvivesCorruptionAndChecksIdentity(t *testing.T) {
	dir := t.TempDir()
	r := Recovery{ID: RecoveryID("/root", "/root/file"), Workspace: "/root", Path: "/root/file", Text: "草稿", Version: 9}
	if e := WriteRecovery(dir, r); e != nil {
		t.Fatal(e)
	}
	got, e := Recoveries(dir)
	if e != nil || len(got) != 1 || got[0].Text != r.Text {
		t.Fatal(got, e)
	}
	info, _ := os.Stat(filepath.Join(dir, r.ID+".json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	path := filepath.Join(dir, r.ID+".json")
	os.WriteFile(path, []byte("broken"), 0600)
	if _, e = Recoveries(dir); e == nil {
		t.Fatal("corruption ignored")
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("corrupt copy deleted")
	}
	r.ID = "../escape"
	if WriteRecovery(dir, r) == nil {
		t.Fatal("unsafe id accepted")
	}
}

func TestInterruptedCheckpointAndCapacityFailureKeepLastDraft(t *testing.T) {
	dir := t.TempDir()
	r := Recovery{ID: RecoveryID("/workspace", "/workspace/file"), Workspace: "/workspace", Path: "/workspace/file", Text: "已完成的检查点", Version: 1}
	if err := WriteRecovery(dir, r); err != nil {
		t.Fatal(err)
	}
	// A process interrupted before rename leaves a temporary file, not a new record.
	if err := os.WriteFile(filepath.Join(dir, ".checkpoint-interrupted"), []byte(`{"Text":`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Recoveries(dir)
	if err != nil || len(got) != 1 || got[0].Text != r.Text {
		t.Fatal(got, err)
	}
	quota, err := os.Create(filepath.Join(dir, ".capacity-fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if err := quota.Truncate(256 << 20); err != nil {
		quota.Close()
		t.Fatal(err)
	}
	quota.Close()
	next := r
	next.Text, next.Version = "尚未完成的检查点", 2
	if err := WriteRecovery(dir, next); err == nil {
		t.Fatal("capacity failure was not reported")
	}
	got, err = Recoveries(dir)
	if err != nil || len(got) != 1 || got[0].Text != r.Text {
		t.Fatal("failed checkpoint lost completed draft", got, err)
	}
	if err := os.Remove(filepath.Join(dir, ".capacity-fixture")); err != nil {
		t.Fatal(err)
	}
	if err := WriteRecovery(dir, next); err != nil {
		t.Fatal(err)
	}
	got, err = Recoveries(dir)
	if err != nil || len(got) != 1 || got[0].Text != next.Text {
		t.Fatal("completed replacement was not recoverable", got, err)
	}
}

func TestSaveDetectsSameSizeSameTimestampExternalEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	base, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if err := os.WriteFile(path, []byte("disk"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(context.Background(), base, "mine", false); !errors.Is(err, ErrConflict) {
		t.Fatal("same-size external content escaped hash check", err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "disk" {
		t.Fatal("external content overwritten")
	}
}

func TestSaveRejectsParentReplacedByLinkToMovedDirectory(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "source")
	moved := filepath.Join(root, "moved")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "file")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	base, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, parent); err != nil {
		t.Fatal(err)
	}
	// The file's inode, content and mode are unchanged, but its path is redirected.
	if _, err := Save(context.Background(), base, "must not write", false); !errors.Is(err, ErrConflict) {
		t.Fatal("redirected directory accepted", err)
	}
	raw, _ := os.ReadFile(filepath.Join(moved, "file"))
	if string(raw) != "original" {
		t.Fatal("save followed redirected parent")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(context.Background(), base, "must not recreate", true); !errors.Is(err, ErrConflict) {
		t.Fatal("recreation followed redirected parent", err)
	}
	if _, err := os.Stat(filepath.Join(moved, "file")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("redirected missing file was recreated", err)
	}
}
