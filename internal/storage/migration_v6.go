package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/shenming0821/Fuyu/internal/agent"
)

const retiredAgent = "devin"

const sessionsV6SQL = `
CREATE TABLE sessions_v6 (
	id INTEGER PRIMARY KEY,
	agent TEXT NOT NULL DEFAULT 'codex' CHECK(agent IN ('codex','claude')),
	thread_id TEXT, cwd TEXT NOT NULL, title TEXT NOT NULL, payload BLOB NOT NULL,
	UNIQUE(agent, thread_id));
INSERT INTO sessions_v6(id,agent,thread_id,cwd,title,payload)
	SELECT id,agent,thread_id,cwd,title,payload FROM sessions WHERE agent IN ('codex','claude');
DROP TABLE sessions;
ALTER TABLE sessions_v6 RENAME TO sessions;
CREATE INDEX sessions_cwd ON sessions(cwd);
`

const runtimeV6SQL = `
CREATE TABLE runtime_identity_v6 (
	agent TEXT PRIMARY KEY CHECK(agent IN ('codex','claude')), home TEXT NOT NULL);
INSERT INTO runtime_identity_v6(agent,home)
	SELECT agent,home FROM runtime_identity WHERE agent IN ('codex','claude');
DROP TABLE runtime_identity;
ALTER TABLE runtime_identity_v6 RENAME TO runtime_identity;
`

func (s *Store) migrateV6(backupExisting bool) error {
	if backupExisting {
		path, err := s.vacuumBackup("fuyu-before-v6-*.db")
		if err != nil {
			return err
		}
		if err := validateBackupVersion(path, 5); err != nil {
			return fmt.Errorf("v6 迁移备份不可用 %s：%w", filepath.Base(path), err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	removedSessions, removedHomes, err := retiredAgentRows(tx)
	if err != nil {
		return err
	}
	settings, err := retiredAgentSettings(tx, removedSessions, removedHomes)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(sessionsV6SQL); err != nil {
		return fmt.Errorf("移除 Devin 会话失败，事务已回滚：%w", err)
	}
	if _, err := tx.Exec(runtimeV6SQL); err != nil {
		return fmt.Errorf("移除 Devin Runtime 归属失败，事务已回滚：%w", err)
	}
	if settings != nil {
		if _, err := tx.Exec("UPDATE settings SET payload=? WHERE id=1", settings); err != nil {
			return fmt.Errorf("清理 Devin 界面设置失败，事务已回滚：%w", err)
		}
	}
	if _, err := tx.Exec("PRAGMA user_version=6"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(s.dir, retiredAgent)); err != nil {
		return fmt.Errorf("清理 Devin 应用数据失败：%w", err)
	}
	return nil
}

func (s *Store) vacuumBackup(pattern string) (string, error) {
	f, err := os.CreateTemp(s.dir, pattern)
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	if _, err := s.db.Exec("VACUUM INTO ?", path); err != nil {
		return "", fmt.Errorf("v6 迁移前备份 %s：%w", path, err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return "", err
	}
	return path, nil
}

func retiredAgentRows(tx *sql.Tx) (map[int]bool, map[string]bool, error) {
	sessions := map[int]bool{}
	rows, err := tx.Query("SELECT id FROM sessions WHERE agent=?", retiredAgent)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		sessions[id] = true
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	homes := map[string]bool{}
	rows, err = tx.Query("SELECT home FROM runtime_identity WHERE agent=?", retiredAgent)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var home string
		if err := rows.Scan(&home); err != nil {
			rows.Close()
			return nil, nil, err
		}
		homes[home] = true
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return sessions, homes, nil
}

func retiredAgentSettings(tx *sql.Tx, sessions map[int]bool, homes map[string]bool) ([]byte, error) {
	var raw []byte
	err := tx.QueryRow("SELECT payload FROM settings WHERE id=1").Scan(&raw)
	if err == sql.ErrNoRows || len(raw) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("v6 迁移前读取界面设置：%w", err)
	}
	if snapshot.DefaultAgent.Normalized() == agent.AgentKind(retiredAgent) {
		snapshot.DefaultAgent = agent.Codex
	}
	snapshot.DeletedThreads = slices.DeleteFunc(snapshot.DeletedThreads, func(item DeletedThread) bool {
		return item.Agent.Normalized() == agent.AgentKind(retiredAgent)
	})
	snapshot.LastClaudeModels = withoutRetiredKeys(snapshot.LastClaudeModels, homes)
	snapshot.ModelEfforts = withoutRetiredKeys(snapshot.ModelEfforts, nil)
	snapshot.ModelDisplayNames = withoutRetiredKeys(snapshot.ModelDisplayNames, nil)

	deletedTabs := map[string]bool{}
	for id := range sessions {
		deletedTabs["session-"+strconv.Itoa(id)] = true
	}
	snapshot.OpenTabs = slices.DeleteFunc(snapshot.OpenTabs, func(id int) bool { return sessions[id] })
	snapshot.WorkspaceTabs = slices.DeleteFunc(snapshot.WorkspaceTabs, func(tab Tab) bool {
		return tab.Kind == "session" && sessions[tab.SessionID]
	})
	if deletedTabs[snapshot.ActiveTabID] {
		snapshot.ActiveTabID = ""
	}
	if sessions[snapshot.Selected] {
		snapshot.Selected = 0
		if len(snapshot.OpenTabs) > 0 {
			snapshot.Selected = snapshot.OpenTabs[0]
		}
	}
	for key, value := range snapshot.FolderSelectedTabs {
		if deletedTabs[value] {
			delete(snapshot.FolderSelectedTabs, key)
		}
	}
	for key, layout := range snapshot.WorkspaceLayouts {
		retirePaneTabs(layout.Root, deletedTabs)
		snapshot.WorkspaceLayouts[key] = layout
	}
	return json.Marshal(snapshot)
}

func withoutRetiredKeys(values map[string]string, homes map[string]bool) map[string]string {
	for key := range values {
		if strings.HasPrefix(key, retiredAgent+"\x00") || homes[key] {
			delete(values, key)
		}
	}
	return values
}

func retirePaneTabs(node *PaneNode, deleted map[string]bool) {
	if node == nil {
		return
	}
	node.Tabs = slices.DeleteFunc(node.Tabs, func(id string) bool { return deleted[id] })
	if deleted[node.Active] {
		node.Active = ""
	}
	retirePaneTabs(node.First, deleted)
	retirePaneTabs(node.Second, deleted)
}
