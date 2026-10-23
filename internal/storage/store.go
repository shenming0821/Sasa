package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	_ "modernc.org/sqlite"
)

// Only application-owned metadata belongs here. Messages and credentials stay
// with Codex; pending protocol requests and secret form answers are never saved.
type Session struct {
	ID           int
	ThreadID     string
	Cwd          string
	Title        string
	Draft        string
	DraftSkills  map[string]string
	TurnID       string
	Active       bool
	Anchor       string
	AtEnd        bool
	ScrollY      float32
	AnchorOffset float32
	Expanded     map[string]bool
}

type Snapshot struct {
	Selected          int
	OpenTabs          []int
	SidebarOpen       bool
	SidebarWidth      float32
	HistoryOpen       bool
	CollapsedProjects map[string]bool
	Sessions          []Session
}

type Store struct {
	db   *sql.DB
	lock *os.File
}

func Open(dir string) (_ *Store, err error) {
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "fuyu.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("Fuyu 数据目录正在使用：%w", err)
	}
	s := &Store{lock: lock}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	path := filepath.Join(dir, "fuyu.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	s.db, err = sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s.db.SetMaxOpenConns(1)
	if _, err = s.db.Exec("PRAGMA busy_timeout=250; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;"); err != nil {
		return nil, err
	}
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return nil, err
	}
	if version > 2 {
		return nil, fmt.Errorf("数据库版本 %d 高于当前支持版本 2，保留原文件", version)
	}
	if version == 0 {
		tx, e := s.db.Begin()
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		if _, err = tx.Exec(`CREATE TABLE sessions (
			id INTEGER PRIMARY KEY, thread_id TEXT UNIQUE, cwd TEXT NOT NULL,
			title TEXT NOT NULL, payload BLOB NOT NULL);
			CREATE INDEX sessions_cwd ON sessions(cwd);
			CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK(id=1), payload BLOB NOT NULL);
			PRAGMA user_version=1;`); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	}
	if version < 2 {
		tx, e := s.db.Begin()
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		if _, err = tx.Exec("CREATE TABLE runtime_identity (id INTEGER PRIMARY KEY CHECK(id=1), codex_home TEXT NOT NULL); PRAGMA user_version=2;"); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) BindRuntime(home string) error {
	var saved string
	err := s.db.QueryRow("SELECT codex_home FROM runtime_identity WHERE id=1").Scan(&saved)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec("INSERT INTO runtime_identity(id,codex_home) VALUES(1,?)", home)
		return err
	}
	if err != nil {
		return err
	}
	if saved != home {
		return fmt.Errorf("应用索引绑定 Codex 目录 %s，当前为 %s；请使用独立的 -data-dir", saved, home)
	}
	return nil
}

func (s *Store) Close() error {
	var err error
	if s.db != nil {
		err = s.db.Close()
	}
	if s.lock != nil {
		syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		s.lock.Close()
	}
	return err
}

func (s *Store) Load() (Snapshot, error) {
	var result Snapshot
	var raw []byte
	err := s.db.QueryRow("SELECT payload FROM settings WHERE id=1").Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return result, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			return result, fmt.Errorf("读取界面设置：%w", err)
		}
	}
	rows, err := s.db.Query("SELECT payload FROM sessions ORDER BY id")
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Session
		if err := rows.Scan(&raw); err != nil {
			return result, err
		}
		if err := json.Unmarshal(raw, &item); err != nil || item.ID <= 0 {
			return result, fmt.Errorf("会话索引损坏，保留原记录：%v", err)
		}
		result.Sessions = append(result.Sessions, item)
	}
	return result, rows.Err()
}

func (s *Store) Save(snapshot Snapshot) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range snapshot.Sessions {
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		var thread any
		if item.ThreadID != "" {
			thread = item.ThreadID
		}
		if _, err := tx.Exec(`INSERT INTO sessions(id,thread_id,cwd,title,payload) VALUES(?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET thread_id=excluded.thread_id,cwd=excluded.cwd,title=excluded.title,payload=excluded.payload`,
			item.ID, thread, item.Cwd, item.Title, raw); err != nil {
			return err
		}
	}
	snapshot.Sessions = nil
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO settings(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", raw); err != nil {
		return err
	}
	return tx.Commit()
}
