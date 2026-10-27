package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func legacyV3(t *testing.T, dir string) []byte {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "fuyu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sessions (
		id INTEGER PRIMARY KEY, thread_id TEXT UNIQUE, cwd TEXT NOT NULL,
		title TEXT NOT NULL, payload BLOB NOT NULL);
		CREATE INDEX sessions_cwd ON sessions(cwd);
		CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK(id=1), payload BLOB NOT NULL);
		CREATE TABLE runtime_identity (id INTEGER PRIMARY KEY CHECK(id=1), codex_home TEXT NOT NULL);
		INSERT INTO runtime_identity VALUES(1,'/codex/original');
		PRAGMA user_version=3;`); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"ID":1,"ThreadID":"native","Cwd":"/project","Title":"旧会话","Draft":"保留草稿","SelectedModel":"old-model","Active":true,"TurnID":"old-turn"}`)
	if _, err := db.Exec("INSERT INTO sessions VALUES(1,'native','/project','旧会话',?)", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO settings VALUES(1,?)", []byte(`{"Selected":1,"OpenTabs":[1]}`)); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestV4MigratesCodexIdentityWithoutChangingPayload(t *testing.T) {
	dir := t.TempDir()
	raw := legacyV3(t, dir)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatal("未迁移至 v4", version, err)
	}
	var payload []byte
	var owner string
	if err := s.db.QueryRow("SELECT agent,payload FROM sessions WHERE id=1").Scan(&owner, &payload); err != nil ||
		owner != "codex" || string(payload) != string(raw) {
		t.Fatal("旧原生归属或 payload 变化", owner, err)
	}
	snapshot, err := s.Load()
	if err != nil || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Agent.Normalized() != agent.Codex ||
		snapshot.Sessions[0].Draft != "保留草稿" || !snapshot.Sessions[0].Active {
		t.Fatal("旧数据丢失", snapshot, err)
	}
	if err := s.BindRuntime("/codex/original"); err != nil {
		t.Fatal(err)
	}
	if s.BindRuntime("/codex/wrong") == nil {
		t.Fatal("迁移绕过旧 home 绑定")
	}
	if err := s.BindAgentRuntime(agent.Claude, "/claude/home"); err != nil {
		t.Fatal(err)
	}
	if s.BindAgentRuntime(agent.Claude, "/claude/wrong") == nil {
		t.Fatal("Claude home 绑定失效")
	}
	backups, err := filepath.Glob(filepath.Join(dir, "fuyu-before-v4-*.db"))
	if err != nil || len(backups) != 1 {
		t.Fatal("缺少本次一致性备份", backups, err)
	}
	if err := validateBackupVersion(backups[0], 3); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(backups[0])
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("备份权限不正确", err)
	}
}

func TestV4NamespacesNativeIDsAndRollsBackDuplicate(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{Selected: 1, Sessions: []Session{
		{ID: 1, ThreadID: "same", Cwd: "/one", SelectedModel: "one"},
		{ID: 2, Agent: agent.Claude, ModelAgent: agent.Claude, ThreadID: "same", Cwd: "/two", SelectedModel: "two"},
		{ID: 3, Agent: agent.Claude, Cwd: "/three"},
		{ID: 4, Agent: agent.Claude, Cwd: "/four"},
	}}
	if err := s.Save(want); err != nil {
		t.Fatal("跨 Agent 原生 ID 应可相同；空 ID 不占唯一约束", err)
	}
	bad := want
	bad.Selected = 2
	bad.Sessions = append(append([]Session(nil), want.Sessions...), Session{ID: 5, Agent: agent.Claude, ThreadID: "same"})
	if s.Save(bad) == nil {
		t.Fatal("同一 Agent 的重复原生 ID 应被拒绝")
	}
	got, err := s.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("失败事务改写原快照", got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("重开串用 Agent 或模型", got, err)
	}
}

func TestV4MigrationFailurePreservesSchemaAndLegacyRows(t *testing.T) {
	dir := t.TempDir()
	raw := legacyV3(t, dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "fuyu.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE runtime_identity RENAME COLUMN codex_home TO unexpected_home"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(dir); err == nil {
		s.Close()
		t.Fatal("无效旧表应导致迁移失败")
	}
	db, err = sql.Open("sqlite", filepath.Join(dir, "fuyu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, hasAgent int
	var preserved []byte
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatal("失败推进版本", version, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('sessions') WHERE name='agent'").Scan(&hasAgent); err != nil || hasAgent != 0 {
		t.Fatal("会话表重建未回滚", hasAgent, err)
	}
	if err := db.QueryRow("SELECT payload FROM sessions WHERE id=1").Scan(&preserved); err != nil || string(preserved) != string(raw) {
		t.Fatal("失败损失旧 payload", err)
	}
}

func TestUnknownAgentAndForeignModelDoNotOverwriteSnapshot(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, item := range []Session{
		{ID: 1, Agent: "unknown"},
		{ID: 1, Agent: agent.Claude, ModelAgent: agent.Codex, SelectedModel: "codex-model"},
	} {
		if s.Save(Snapshot{Sessions: []Session{item}}) == nil {
			t.Fatal("未知 Agent 或外来模型不能写入", item)
		}
	}
	got, err := s.Load()
	raw, _ := json.Marshal(got.Sessions)
	if err != nil || len(got.Sessions) != 0 {
		t.Fatal("失败保存留下会话", string(raw), err)
	}
}

func TestRuntimeBindingCanonicalizesAgentHome(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := t.TempDir()
	home := filepath.Join(dir, "native")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	if err := s.BindAgentRuntime(agent.Claude, link); err != nil {
		t.Fatal(err)
	}
	if err := s.BindAgentRuntime(agent.Claude, home); err != nil {
		t.Fatal("同一规范化目录不应被误判为另一个 Runtime", err)
	}
	if s.BindAgentRuntime("unknown", home) == nil || s.BindAgentRuntime(agent.Claude, "") == nil {
		t.Fatal("无效身份不能绑定")
	}
}
