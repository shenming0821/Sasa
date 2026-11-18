package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

const sessionsV4SQL = `
	CREATE TABLE sessions_v4 (
		id INTEGER PRIMARY KEY,
		agent TEXT NOT NULL DEFAULT 'codex' CHECK(agent IN ('codex','claude')),
		thread_id TEXT, cwd TEXT NOT NULL, title TEXT NOT NULL, payload BLOB NOT NULL,
		UNIQUE(agent, thread_id));
	INSERT INTO sessions_v4(id,agent,thread_id,cwd,title,payload)
		SELECT id,'codex',NULLIF(thread_id,''),cwd,title,payload FROM sessions;
	DROP TABLE sessions;
	ALTER TABLE sessions_v4 RENAME TO sessions;
	CREATE INDEX sessions_cwd ON sessions(cwd);
`

const runtimeV4SQL = `
	CREATE TABLE runtime_identity_v4 (
		agent TEXT PRIMARY KEY CHECK(agent IN ('codex','claude')), home TEXT NOT NULL);
	INSERT INTO runtime_identity_v4(agent,home)
		SELECT 'codex',codex_home FROM runtime_identity;
	DROP TABLE runtime_identity;
	ALTER TABLE runtime_identity_v4 RENAME TO runtime_identity;
`

func (s *Store) migrateV4(backupExisting bool) error {
	if _, err := s.Load(); err != nil {
		return fmt.Errorf("v4 迁移前读取索引，保留原数据库：%w", err)
	}
	if backupExisting {
		// A unique backup per attempt includes committed WAL pages via VACUUM.
		f, err := os.CreateTemp(s.dir, "sasa-before-v4-*.db")
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
			return fmt.Errorf("v4 迁移前备份 %s：%w", path, err)
		}
		if err := os.Chmod(path, 0600); err != nil {
			return err
		}
		if err := validateBackupVersion(path, 3); err != nil {
			return fmt.Errorf("v4 迁移备份不可用 %s：%w", filepath.Base(path), err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hasAgent, hasHome int
	if err := tx.QueryRow("SELECT count(*) FROM pragma_table_info('sessions') WHERE name='agent'").Scan(&hasAgent); err != nil {
		return err
	}
	if hasAgent == 0 {
		if _, err := tx.Exec(sessionsV4SQL); err != nil {
			return fmt.Errorf("迁移 Agent 归属失败，事务已回滚：%w", err)
		}
	}
	if err := tx.QueryRow("SELECT count(*) FROM pragma_table_info('runtime_identity') WHERE name='home'").Scan(&hasHome); err != nil {
		return err
	}
	if hasHome == 0 {
		if _, err := tx.Exec(runtimeV4SQL); err != nil {
			return fmt.Errorf("迁移 Runtime 归属失败，事务已回滚：%w", err)
		}
	}
	if _, err := tx.Exec("PRAGMA user_version=4"); err != nil {
		return err
	}
	return tx.Commit()
}
