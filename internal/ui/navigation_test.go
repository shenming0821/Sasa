package ui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestProjectNavigationCollapseAndTabs(t *testing.T) {
	a := New(stubBackend{}, "test", "/one/project", nil)
	first := a.session
	first.saved.Title, first.State.Draft = "修复输入", "草稿一"
	a.newSession("/two/project")
	second := a.session
	second.saved.Title, second.State.Draft = "调整布局", "草稿二"
	tt := native.NewTester(a.view, 1400, 800)
	if tt.HasText("搜索会话") || tt.HasText("搜索项目与会话") {
		t.Fatal("sidebar search input still rendered")
	}
	for _, path := range []string{"/one/project", "/two/project"} {
		if !tt.HasText("展开项目 " + path) {
			t.Fatal("same-name projects merged:", path)
		}
	}
	if err := tt.Click("展开项目 /one/project"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if tt.HasText("切换会话 1") {
		t.Fatal("collapsed project still shows sessions")
	}
	tt.Frame()
	if tt.HasText("切换会话 1") || !tt.HasText("切换会话 2") || !a.collapsedProjects["/one/project"] {
		t.Fatal("render changed collapsed state or hid another project's session")
	}
	if err := tt.Click("展开项目 /one/project"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("切换会话 1") || !tt.HasText("切换会话 2") {
		t.Fatal("expanding did not reveal all project sessions")
	}
	if err := tt.Click("切换会话 1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("会话标签 2") {
		t.Fatalf("another folder's tab is visible: selected=%d folder=%q", a.session.id, a.activeWorkspace)
	}
	if err := tt.Click("切换会话 2"); err != nil {
		t.Fatal(err)
	}
	if a.session != second || first.State.Draft != "草稿一" || second.State.Draft != "草稿二" {
		t.Fatal("navigation changed session draft ownership")
	}
}

func TestResponsivePanelsPreserveComposerAndNavigation(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.threadList.threads = []codex.ThreadMeta{{ID: "one", Cwd: "/project", Preview: "历史任务"}}
	tt := native.NewTester(a.view, 1400, 800)
	if err := tt.Click("切换右侧边栏"); err != nil {
		t.Fatal(err)
	}
	wide, _ := tt.Find("任务输入")
	history, ok := tt.Find("搜索历史")
	if !ok || history.X <= wide.X+wide.W {
		t.Fatal("history did not appear beside conversation")
	}
	for _, width := range []int{1000, 640, 1400} {
		tt.SetSize(width, 480)
		input, ok := tt.Find("任务输入")
		history, visible := tt.Find("搜索历史")
		send, sendVisible := tt.Find("发送")
		if !ok || !visible || !sendVisible || history.X <= input.X+input.W ||
			send.X+send.W > history.X || input.W < 300 || input.Y+input.H > 480 {
			t.Fatalf("history overlaps conversation at %d: input=%+v history=%+v send=%+v", width, input, history, send)
		}
		if width == 640 && tt.HasText("添加项目") {
			t.Fatal("narrow window did not collapse project navigation")
		}
		if err := tt.Click("打开历史 one"); err != nil {
			t.Fatalf("at width %d: %v", width, err)
		}
		if !tt.HasText("搜索历史") {
			t.Fatal("opening history closed the docked sidebar")
		}
		if err := tt.Click("切换右侧边栏"); err != nil {
			t.Fatal(err)
		}
		input, ok = tt.Find("任务输入")
		if !ok || input.W < 400 || input.X < 0 || input.X+input.W > float32(width) || input.Y+input.H > 480 {
			t.Fatalf("composer clipped at %d: %+v", width, input)
		}
		if width == 640 {
			if tt.HasText("添加项目") {
				t.Fatal("narrow window did not collapse sidebar")
			}
			if err := tt.Click("切换右侧边栏"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("搜索历史") {
				t.Fatal("narrow history navigation unreachable")
			}
			if err := tt.Click("切换右侧边栏"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("切换左侧边栏"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("添加项目") {
				t.Fatal("narrow project navigation unreachable")
			}
			if err := tt.Click("切换会话 1"); err != nil {
				t.Fatal(err)
			}
			if tt.HasText("添加项目") {
				t.Fatal("temporary navigation did not dismiss on selection")
			}
		}
		if err := tt.Click("切换右侧边栏"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryScopeSearchAndExistingSession(t *testing.T) {
	a := New(stubBackend{}, "test", "/one", nil)
	first := a.session
	first.State.ThreadID, first.State.Draft = "one", "保留草稿"
	a.threadList.threads = []codex.ThreadMeta{
		{ID: "one", Cwd: "/one", Preview: "输入修复"},
		{ID: "two", Cwd: "/two", Preview: "布局修复"},
	}
	a.historyOpen = true
	tt := native.NewTester(a.view, 1400, 800)
	if !tt.HasText("打开历史 one") || tt.HasText("打开历史 two") {
		t.Fatal("current-project filter incorrect")
	}
	if err := tt.Click("全部项目"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("打开历史 two") {
		t.Fatal("all-project filter excluded a project")
	}
	if err := tt.Click("搜索历史"); err != nil {
		t.Fatal(err)
	}
	tt.Type("输入")
	if tt.HasText("打开历史 two") || !tt.HasText("打开历史 one") {
		t.Fatal("history text filter incorrect")
	}
	if err := tt.Click("打开历史 one"); err != nil {
		t.Fatal(err)
	}
	if a.session != first || len(a.sessions) != 1 || first.State.Draft != "保留草稿" {
		t.Fatal("opening existing history duplicated session or lost draft")
	}
}

func TestNavigationSettingsRestoreAndOldDefaults(t *testing.T) {
	for _, savedWidth := range []float32{0, 287} {
		t.Run(fmt.Sprint(savedWidth), func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			a := New(stubBackend{}, "test", "/one", nil)
			if err := a.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			a.sidebarWidth, a.historyOpen = savedWidth, true
			a.rightPanelWidth = savedWidth
			if savedWidth != 0 {
				a.rightPanelWidth = 430
			}
			a.collapsedProjects = map[string]bool{"/one": true}
			if err := a.persist(); err != nil {
				t.Fatal(err)
			}
			b := New(stubBackend{}, "test", "/one", nil)
			if err := b.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			want := savedWidth
			if want == 0 {
				want = 240
			}
			if b.sidebarWidth != want || !b.historyOpen || !b.collapsedProjects["/one"] {
				t.Fatal("layout settings lost or old width default not applied")
			}
			wantRight := float32(320)
			if savedWidth != 0 {
				wantRight = 430
			}
			if b.rightPanelWidth != a.rightPanelWidth || b.effectiveRightPanelWidth(1400) != wantRight {
				t.Fatal("right panel width lost or old default not applied")
			}
		})
	}
}

func TestRightPanelDividerDragLimitsAndSharedTabs(t *testing.T) {
	a := New(stubBackend{}, "test", "/one", nil)
	a.historyOpen = true
	a.threadList.threads = []codex.ThreadMeta{{ID: "one", Cwd: "/one"}}
	tt := native.NewTester(a.view, 1400, 760)
	toggle := func() native.Rect {
		t.Helper()
		r, ok := tt.Find("切换右侧边栏")
		if !ok {
			t.Fatal("missing right panel")
		}
		return r
	}
	drag := func(dx float32) {
		t.Helper()
		r := toggle()
		available := float32(1400)
		if a.sidebarVisible(1400) {
			available -= a.sidebarWidth + 1
		}
		x, y := 1400-a.effectiveRightPanelWidth(available)-1, r.Y+r.H/2
		tt.Move(10, 10)
		idle := tt.Image().RGBAAt(int(x), int(y))
		tt.Move(x, y)
		if tt.Cursor() != native.CursorResizeEW {
			t.Fatal("divider has no horizontal resize cursor")
		}
		if got := tt.Image().RGBAAt(int(x), int(y)); got != idle {
			t.Fatalf("divider changed color on hover: idle=%v hover=%v", idle, got)
		}
		tt.Press(x, y)
		if got := tt.Image().RGBAAt(int(x), int(y)); got != idle {
			t.Fatalf("divider changed color while held: idle=%v held=%v", idle, got)
		}
		tt.Move(x+dx, y)
		tt.Release(x+dx, y)
		tt.Frame()
	}
	tt.Move(10, 10)
	leftIdle := tt.Image().RGBAAt(int(a.sidebarWidth), 200)
	tt.Move(a.sidebarWidth, 200)
	if tt.Cursor() != native.CursorResizeEW || tt.Image().RGBAAt(int(a.sidebarWidth), 200) != leftIdle {
		t.Fatal("left sidebar divider lost resize cursor or highlighted on hover")
	}
	before := toggle().X
	drag(-80)
	if a.rightPanelWidth != 400 || toggle().X != before {
		t.Fatalf("right panel did not grow from its left edge: width=%v", a.rightPanelWidth)
	}
	// A drag focuses this divider; arrow keys must move the same boundary.
	tt.Key(0, native.KeyLeft)
	if a.rightPanelWidth != 410 {
		t.Fatalf("keyboard did not grow right panel: %v", a.rightPanelWidth)
	}
	tt.Key(0, native.KeyRight)
	if a.rightPanelWidth != 400 {
		t.Fatalf("keyboard did not shrink right panel: %v", a.rightPanelWidth)
	}
	for _, tab := range []string{"文件", "Git", "会话历史"} {
		if err := tt.Click(tab); err != nil {
			t.Fatal(err)
		}
		if a.rightPanelWidth != 400 {
			t.Fatal("switching right tabs reset width")
		}
	}
	drag(-500)
	if a.rightPanelWidth != 900 {
		t.Fatalf("right panel did not grow past previous 520 cap: %v", a.rightPanelWidth)
	}
	drag(800)
	if a.rightPanelWidth != 240 {
		t.Fatalf("lower width bound: %v", a.rightPanelWidth)
	}
	a.rightPanelWidth = 500
	tt.Frame()
	tt.SetSize(640, 480)
	if a.rightPanelWidth != 500 || a.effectiveRightPanelWidth(640) != 240 || tt.HasText("添加项目") {
		t.Fatal("narrow window lost preference or failed to fit right panel")
	}
	tt.SetSize(1400, 760)
	if a.effectiveRightPanelWidth(1159) != 500 {
		t.Fatal("wide window did not restore preferred width")
	}
}

func TestSelectedTabRemainsVisibleAndSidebarResizesWithKeyboard(t *testing.T) {
	a := New(stubBackend{}, "test", "/one", nil)
	tt := native.NewTester(a.view, 1000, 760)
	for i := 0; i < 14; i++ {
		a.newSession("/one")
		tt.Frame()
	}
	tab, ok := tt.Find(fmt.Sprintf("会话标签 %d", a.session.id))
	if !ok || tab.X < 241 || tab.X+tab.W > 963 {
		t.Fatalf("selected tab outside viewport: %+v", tab)
	}
	if err := tt.Click("Divider"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, native.KeyRight)
	tt.Key(0, native.KeyRight)
	if a.sidebarWidth <= 240 || a.sidebarWidth > 320 {
		t.Fatalf("divider did not update sidebar width: %v", a.sidebarWidth)
	}
}

func TestSidebarTogglesMoveBetweenPanelHeadersAndConversation(t *testing.T) {
	for _, width := range []int{640, 1000, 1400} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/one", nil)
			a.threadList.threads = []codex.ThreadMeta{{ID: "one", Cwd: "/one", Preview: "历史"}}
			tt := native.NewTester(a.view, width, 760)
			check := func() {
				t.Helper()
				for _, label := range []string{"切换左侧边栏", "切换右侧边栏"} {
					count := 0
					for _, text := range tt.Texts() {
						if text == label {
							count++
						}
					}
					if count != 1 {
						t.Fatalf("%q has %d controls, want one", label, count)
					}
				}
				left, _ := tt.Find("切换左侧边栏")
				project, projectShown := tt.Find("添加项目")
				if projectShown {
					if left.X+left.W > a.sidebarWidth || left.Y >= project.Y {
						t.Fatalf("left toggle outside panel header: toggle=%+v project=%+v", left, project)
					}
				} else if left.X > 40 || left.Y >= 46 {
					t.Fatalf("collapsed left toggle not in conversation toolbar: %+v", left)
				}
				right, _ := tt.Find("切换右侧边栏")
				history, historyShown := tt.Find("搜索历史")
				create, _ := tt.Find("使用 Claude Code 新建会话")
				if historyShown {
					if right.X < history.X || right.Y >= history.Y || create.X+create.W >= history.X {
						t.Fatalf("right toggle outside panel header: toggle=%+v search=%+v", right, history)
					}
				} else if right.Y >= 46 {
					t.Fatalf("collapsed right toggle not in conversation toolbar: %+v", right)
				}
			}
			check()
			for _, label := range []string{
				"切换左侧边栏", "切换左侧边栏",
				"切换右侧边栏", "切换左侧边栏",
				"切换左侧边栏", "切换右侧边栏",
			} {
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				check()
			}
		})
	}
}

func TestProjectNavigationOmitsUnboundSessions(t *testing.T) {
	a := New(stubBackend{}, "test", "", nil)
	unbound := a.session
	unbound.State.Draft = "keep unbound draft"
	tt := native.NewTester(a.view, 1000, 760)
	if tt.HasText("未选择项目") || slices.Contains(tt.Texts(), "展开项目 ") || len(a.projectItems()) != 0 || !tt.HasText("尚未添加项目") {
		t.Fatal("unbound session appeared as a project")
	}
	a.newSession("/real-project")
	tt.Frame()
	if !tt.HasText("展开项目 /real-project") || slices.Contains(tt.Texts(), "展开项目 ") || tt.HasText("尚未添加项目") {
		t.Fatalf("project filtering hid a real directory or retained placeholder: %v; items=%v", tt.Texts(), a.orderedProjectIDs())
	}
	if len(a.projectItems()) != 1 || a.projectItems()[0].group.path != "/real-project" {
		t.Fatal("project list included an unbound session or omitted a real directory")
	}
	if len(a.sessions) != 2 || !unbound.open || unbound.State.Draft != "keep unbound draft" {
		t.Fatal("hiding placeholder changed session or draft")
	}
	a.selectSession(unbound)
	tt.Frame()
	if err := tt.Click("会话标签 1"); err != nil {
		t.Fatal(err)
	}
	if a.session != unbound || a.State.Draft != "keep unbound draft" {
		t.Fatal("unbound session no longer accessible from its tab")
	}
}
