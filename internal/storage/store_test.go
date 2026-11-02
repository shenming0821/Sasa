package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVersionTwoBackupAndMixedTabs(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{Selected: 1, OpenTabs: []int{1}, Sessions: []Session{{ID: 1, Cwd: "/project", Draft: "旧草稿"}}}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("version 2 migration lost state", got, err)
	}
	path := filepath.Join(dir, "fuyu-before-v3.db")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup missing or not private", err)
	}
	backup, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var version int
	if err := backup.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("backup is not the prior schema", version, err)
	}
	want.WorkspaceTabs = []Tab{
		{ID: "session-1", Kind: "session", SessionID: 1, Workspace: "/project"},
		{ID: "file-a", Kind: "file", Workspace: "/project", Path: "/project/a.go", Caret: 7, ScrollY: 100},
		{ID: "diff-a", Kind: "diff", Workspace: "/project", Path: "/project/a.go", Compare: "已暂存"},
	}
	want.ActiveTabID = "file-a"
	want.ActiveWorkspace, want.RightTab = "/project", "git"
	want.FolderSelectedTabs = map[string]string{"/project": "file-a", "/other": "terminal-b"}
	want.ExpandedFiles = map[string][]string{"/project": {"/project/sub"}}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err = s.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("mixed tabs failed round trip", got, err)
	}
}

func TestInvalidMigrationBackupKeepsVersionTwoAndSnapshot(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "corrupt", "empty", "wrong-version"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			s, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			want := Snapshot{Selected: 1, OpenTabs: []int{1}, Sessions: []Session{{ID: 1, Cwd: "/project", Draft: "preserve draft"}}}
			if err := s.Save(want); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec("PRAGMA user_version=2"); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(dir, "fuyu-before-v3.db")
			switch kind {
			case "directory":
				err = os.Mkdir(backup, 0700)
			case "symlink":
				err = os.Symlink(filepath.Join(dir, "fuyu.db"), backup)
			case "corrupt":
				err = os.WriteFile(backup, []byte("not a database"), 0600)
			case "empty":
				err = os.WriteFile(backup, nil, 0600)
			case "wrong-version":
				var db *sql.DB
				db, err = sql.Open("sqlite", backup)
				if err == nil {
					_, err = db.Exec("PRAGMA user_version=3")
					db.Close()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if migrated, err := Open(dir); err == nil {
				migrated.Close()
				t.Fatal("migration accepted an invalid backup")
			} else if !strings.Contains(err.Error(), "备份") {
				t.Fatal("unexpected migration failure", err)
			}
			db, err := sql.Open("sqlite", filepath.Join(dir, "fuyu.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var version int
			if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
				t.Fatal("failed migration advanced the schema", version, err)
			}
			preserved := &Store{db: db}
			got, err := preserved.Load()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("failed migration changed saved state", got, err)
			}
			if err := os.Remove(backup); err != nil {
				t.Fatal(err)
			}
			db.Close()
			retry, err := Open(dir)
			if err != nil {
				t.Fatal("migration could not retry after fixing backup", err)
			}
			defer retry.Close()
			got, err = retry.Load()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("retry lost original snapshot", got, err)
			}
		})
	}
}

func TestMigrationKeepsOldBackupAndSnapshotsLatestVersionTwo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "index # with spaces")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := Snapshot{Selected: 1, Sessions: []Session{{ID: 1, Cwd: "/project", Draft: "old draft"}}}
	if err := s.Save(old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	firstBackup := filepath.Join(dir, "fuyu-before-v3.db")
	if _, err := s.db.Exec("VACUUM INTO ?", firstBackup); err != nil {
		t.Fatal(err)
	}
	latest := Snapshot{Selected: 1, Sessions: []Session{{ID: 1, Cwd: "/project", Draft: "latest draft after retry"}}}
	if err := s.Save(latest); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fresh, err := filepath.Glob(filepath.Join(dir, "fuyu-before-v3-*.db"))
	if err != nil || len(fresh) != 1 {
		t.Fatal("migration did not create a fresh backup", fresh, err)
	}
	for path, want := range map[string]Snapshot{firstBackup: old, fresh[0]: latest} {
		if err := validateMigrationBackup(path); err != nil {
			t.Fatal("backup not valid schema 2", path, err)
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := (&Store{db: db}).Load()
		db.Close()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("backup did not preserve its own snapshot", path, got, err)
		}
	}
	got, err := s.Load()
	if err != nil || !reflect.DeepEqual(got, latest) {
		t.Fatal("migration lost latest snapshot", got, err)
	}
}

func TestUnreadableVersionTwoPayloadDoesNotAdvanceMigration(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Snapshot{Sessions: []Session{{ID: 1, Cwd: "/project"}}}); err != nil {
		t.Fatal(err)
	}
	broken := []byte(`{"WorkspaceTabs":`)
	if _, err := s.db.Exec("UPDATE settings SET payload=?; PRAGMA user_version=2", broken); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if migrated, err := Open(dir); err == nil {
		migrated.Close()
		t.Fatal("migration accepted an unreadable snapshot")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "fuyu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	var payload []byte
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("failed payload validation advanced schema", version, err)
	}
	if err := db.QueryRow("SELECT payload FROM settings WHERE id=1").Scan(&payload); err != nil || string(payload) != string(broken) {
		t.Fatal("failed migration changed corrupt evidence", string(payload), err)
	}
}

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
