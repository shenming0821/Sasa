package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestV6RemovesRetiredAgentDataAndSettings(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(dir, "sasa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		DROP TABLE sessions;
		DROP TABLE runtime_identity;
		CREATE TABLE sessions (
			id INTEGER PRIMARY KEY,
			agent TEXT NOT NULL DEFAULT 'codex' CHECK(agent IN ('codex','claude','devin')),
			thread_id TEXT, cwd TEXT NOT NULL, title TEXT NOT NULL, payload BLOB NOT NULL,
			UNIQUE(agent, thread_id));
		CREATE INDEX sessions_cwd ON sessions(cwd);
		CREATE TABLE runtime_identity (
			agent TEXT PRIMARY KEY CHECK(agent IN ('codex','claude','devin')), home TEXT NOT NULL);
		INSERT INTO runtime_identity VALUES
			('codex','/codex'),('claude','/claude'),('devin','/legacy-runtime');
		PRAGMA user_version=5;
	`); err != nil {
		t.Fatal(err)
	}
	for _, item := range []Session{
		{ID: 1, Agent: agent.Codex, Cwd: "/project", ThreadID: "codex-thread", Title: "Codex"},
		{ID: 2, Agent: agent.AgentKind(retiredAgent), Runtime: "/legacy-runtime", Cwd: "/project", ThreadID: "retired-thread", Title: "Retired", ModelAgent: agent.AgentKind(retiredAgent)},
		{ID: 3, Agent: agent.Claude, Cwd: "/project", ThreadID: "claude-thread", Title: "Claude"},
	} {
		payload, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO sessions VALUES(?,?,?,?,?,?)", item.ID, item.Agent, item.ThreadID, item.Cwd, item.Title, payload); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := json.Marshal(Snapshot{
		DefaultAgent:    agent.AgentKind(retiredAgent),
		Selected:        2,
		OpenTabs:        []int{1, 2, 3},
		ActiveTabID:     "session-2",
		ActiveWorkspace: "/project",
		DeletedThreads: []DeletedThread{
			{Agent: agent.AgentKind(retiredAgent), Runtime: "/legacy-runtime", ThreadID: "retired-thread"},
			{Agent: agent.Claude, Runtime: "/claude", ThreadID: "keep-thread"},
		},
		LastClaudeModels: map[string]string{
			retiredAgent + "\x00/legacy-runtime": "retired-model",
			"/legacy-runtime":                    "unscoped-retired-model",
			"claude\x00/claude":                  "claude-model",
		},
		ModelEfforts: map[string]string{
			retiredAgent + "\x00/legacy-runtime\x00model": "high",
			"claude\x00/claude\x00model":                  "low",
		},
		ModelDisplayNames: map[string]string{
			retiredAgent + "\x00/legacy-runtime\x00/project\x00model": "Retired",
			"claude\x00/claude\x00/project\x00model":                  "Claude",
		},
		WorkspaceTabs: []Tab{
			{ID: "session-1", Kind: "session", SessionID: 1, Workspace: "/project"},
			{ID: "session-2", Kind: "session", SessionID: 2, Workspace: "/project"},
			{ID: "session-3", Kind: "session", SessionID: 3, Workspace: "/project"},
			{ID: "terminal-1", Kind: "terminal", Workspace: "/project"},
		},
		FolderSelectedTabs: map[string]string{"/project": "session-2"},
		WorkspaceLayouts: map[string]WorkspaceLayout{
			"/project": {
				Root: &PaneNode{ID: "pane-1", Tabs: []string{"session-1", "session-2"}, Active: "session-2"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO settings(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", settings); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, retiredAgent, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	rows, err := store.db.Query("SELECT agent FROM sessions ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	var agents []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatal(err)
		}
		agents = append(agents, kind)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0] != "codex" || agents[1] != "claude" {
		t.Fatalf("remaining agents=%v", agents)
	}
	if _, err := store.db.Exec("INSERT INTO runtime_identity VALUES(?,?)", retiredAgent, "/other"); err == nil {
		t.Fatal("retired runtime remains accepted")
	}
	if _, err := store.db.Exec("INSERT INTO sessions VALUES(4,?,?,?,?,?)", retiredAgent, "retired-thread-2", "/project", "Retired", []byte(`{}`)); err == nil {
		t.Fatal("retired session remains accepted")
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.DefaultAgent != agent.Codex || snapshot.Selected != 1 ||
		len(snapshot.OpenTabs) != 2 || snapshot.OpenTabs[0] != 1 || snapshot.OpenTabs[1] != 3 ||
		snapshot.ActiveTabID != "" || len(snapshot.WorkspaceTabs) != 3 ||
		len(snapshot.DeletedThreads) != 1 || snapshot.FolderSelectedTabs["/project"] != "" {
		t.Fatalf("settings not cleaned: %+v", snapshot)
	}
	root := snapshot.WorkspaceLayouts["/project"].Root
	if root == nil || len(root.Tabs) != 1 || root.Tabs[0] != "session-1" || root.Active != "" {
		t.Fatalf("pane layout not cleaned: %+v", root)
	}
	if len(snapshot.LastClaudeModels) != 1 || len(snapshot.ModelEfforts) != 1 ||
		len(snapshot.ModelDisplayNames) != 1 {
		t.Fatalf("retired model preferences remain: %+v", snapshot)
	}
	if _, ok := snapshot.LastClaudeModels["/legacy-runtime"]; ok {
		t.Fatal("retired runtime model preference remains")
	}
	if _, ok := snapshot.ModelEfforts[retiredAgent+"\x00/legacy-runtime\x00model"]; ok {
		t.Fatal("retired model effort remains")
	}
	if _, ok := snapshot.ModelDisplayNames[retiredAgent+"\x00/legacy-runtime\x00/project\x00model"]; ok {
		t.Fatal("retired model display name remains")
	}
	if _, err := os.Stat(filepath.Join(dir, retiredAgent)); !os.IsNotExist(err) {
		t.Fatalf("retired application data remains: %v", err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "sasa-before-v6-*.db"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	if err := validateBackupVersion(backups[0], 5); err != nil {
		t.Fatal(err)
	}
}
