package ui

import (
	native "github.com/egoist/mygo/ui"
	"testing"

	"github.com/shenming0821/Fuyu/internal/codex"
)

func TestDebugHistoryRender(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.nativeThreads = []codex.ThreadMeta{{ID: "one", Cwd: "/project", Preview: "历史任务"}}
	t.Logf("kind=%q runtimeKey=%q cwd=%q scope=%d threads=%d", a.kind, a.runtimeKey, a.State.Cwd, a.historyScope, len(a.nativeThreads))
	tt := native.NewTester(a.view, 1400, 800)
	if err := tt.Click("切换右侧边栏"); err != nil {
		t.Fatal(err)
	}
	t.Logf("has 打开历史 one: %v", tt.HasText("打开历史 one"))
	t.Logf("workspaceCwd=%q", a.workspaceCwd())
	w := a.workspace(a.workspaceRoot(a.workspaceCwd()))
	t.Logf("historyQuery=%q scope=%d", w.historyQuery, w.historyScope)
}
