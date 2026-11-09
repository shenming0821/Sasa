package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// v5 widens the agent CHECK on sessions and runtime_identity for Devin. Both
// tables are rebuilt because SQLite cannot alter a CHECK constraint in place.
const sessionsV5SQL = `
CREATE TABLE sessions_v5 (
	id INTEGER PRIMARY KEY,
	agent TEXT NOT NULL DEFAULT 'codex' CHECK(agent IN ('codex','claude','devin')),
	thread_id TEXT, cwd TEXT NOT NULL, title TEXT NOT NULL, payload BLOB NOT NULL,
	UNIQUE(agent, thread_id));
INSERT INTO sessions_v5(id,agent,thread_id,cwd,title,payload)
	SELECT id,agent,thread_id,cwd,title,payload FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_v5 RENAME TO sessions;
CREATE INDEX sessions_cwd ON sessions(cwd);
`

const runtimeV5SQL = `
CREATE TABLE runtime_identity_v5 (
	agent TEXT PRIMARY KEY CHECK(agent IN ('codex','claude','devin')), home TEXT NOT NULL);
INSERT INTO runtime_identity_v5(agent,home)
	SELECT agent,home FROM runtime_identity;
DROP TABLE runtime_identity;
ALTER TABLE runtime_identity_v5 RENAME TO runtime_identity;
`

func (s *Store) migrateV5(backupExisting bool) error {
	if backupExisting {
		f, err := os.CreateTemp(s.dir, "fuyu-before-v5-*.db")
		if err != nil {
			return err
		}
		path := f.Name()
		if err := f.Close(); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		if _, err := s.db.Exec("VACUUM INTO ?", path); err != nil {
			return fmt.Errorf("v5 迁移前备份 %s：%w", path, err)
		}
		if err := os.Chmod(path, 0600); err != nil {
			return err
		}
		if err := validateBackupVersion(path, 4); err != nil {
			return fmt.Errorf("v5 迁移备份不可用 %s：%w", filepath.Base(path), err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(sessionsV5SQL); err != nil {
		return fmt.Errorf("扩展 sessions Agent 约束失败，事务已回滚：%w", err)
	}
	if _, err := tx.Exec(runtimeV5SQL); err != nil {
		return fmt.Errorf("扩展 Runtime 归属约束失败，事务已回滚：%w", err)
	}
	if _, err := tx.Exec("PRAGMA user_version=5"); err != nil {
		return err
	}
	return tx.Commit()
}
