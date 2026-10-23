package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReopenDraftsMetadataAndExclusiveOwner(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{Selected: 2, OpenTabs: []int{1, 2}, SidebarOpen: true, Sessions: []Session{
		{ID: 1, ThreadID: "native-first", Cwd: "/first", Draft: "unsent\n任务", Active: true, TurnID: "turn",
			Anchor: "message", ScrollY: 30, Expanded: map[string]bool{"tool": true}},
		{ID: 2, Cwd: "/second", Draft: "second"},
	}}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(dir); err == nil {
		other.Close()
		t.Fatal("second process can overwrite the same application index")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.BindRuntime("/codex/one"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindRuntime("/codex/two"); err == nil {
		t.Fatal("index accepted a different Codex home")
	}
	got, err := s.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	info, _ := os.Stat(filepath.Join(dir, "fuyu.db"))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database permissions: %v", info.Mode())
	}
	// A uniqueness failure rolls back the whole snapshot, including settings.
	bad := want
	bad.Selected = 1
	bad.Sessions = append([]Session(nil), want.Sessions...)
	bad.Sessions[1].ThreadID = "native-first"
	if err := s.Save(bad); err == nil {
		t.Fatal("duplicate native owner accepted")
	}
	got, err = s.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("failed transaction changed the prior snapshot")
	}
}

func TestFutureSchemaAndCorruptPayloadArePreserved(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Snapshot{Sessions: []Session{{ID: 1, Cwd: "/first"}}}); err != nil {
		t.Fatal(err)
	}
	s.db.Exec("UPDATE sessions SET payload='broken'")
	if _, err := s.Load(); err == nil {
		t.Fatal("corrupt metadata silently reset")
	}
	var raw string
	s.db.QueryRow("SELECT payload FROM sessions WHERE id=1").Scan(&raw)
	if raw != "broken" {
		t.Fatal("corrupt data overwritten")
	}
	s.db.Exec("PRAGMA user_version=99")
	s.Close()
	if s, err := Open(dir); err == nil {
		s.Close()
		t.Fatal("future schema opened for writing")
	}
}

func TestVersionOneMigratesWithoutLosingDraft(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Snapshot{Sessions: []Session{{ID: 1, Cwd: "/first", Draft: "keep draft"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE runtime_identity; PRAGMA user_version=1;"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snapshot, err := s.Load()
	if err != nil || snapshot.Sessions[0].Draft != "keep draft" {
		t.Fatal("migration lost draft", err)
	}
	if err := s.BindRuntime("/codex/home"); err != nil {
		t.Fatal("migration lacks runtime identity table", err)
	}
}
