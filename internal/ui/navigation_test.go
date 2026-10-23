package ui

import (
	"fmt"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestProjectNavigationSearchCollapseAndTabs(t *testing.T) {
	a := New(stubBackend{}, "test", "/one/project", nil)
	first := a.session
	first.saved.Title, first.State.Draft = "修复输入", "草稿一"
	a.newSession("/two/project")
	second := a.session
	second.saved.Title, second.State.Draft = "调整布局", "草稿二"
	tt := native.NewTester(a.view, 1400, 800)
	for _, path := range []string{"/one/project", "/two/project"} {
		if !tt.HasText("展开项目 " + path) {
			t.Fatal("same-name projects merged:", path)
		}
	}
	if err := tt.Click("展开项目 /one/project"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("切换会话 1") {
		t.Fatal("collapsed project still shows sessions")
	}
	if err := tt.Click("搜索会话"); err != nil {
		t.Fatal(err)
	}
	tt.Type("修复")
	if !tt.HasText("切换会话 1") || tt.HasText("切换会话 2") {
		t.Fatal("search failed to reveal matching collapsed session")
	}
	if err := tt.Click("切换会话 1"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("会话标签 2"); err != nil {
		t.Fatal(err)
	}
	if a.session != second || first.State.Draft != "草稿一" || second.State.Draft != "草稿二" {
		t.Fatal("navigation changed session draft ownership")
	}
}

func TestResponsivePanelsPreserveComposerAndNavigation(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.nativeThreads = []codex.ThreadMeta{{ID: "one", Cwd: "/project", Preview: "历史任务"}}
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
		if width == 640 && tt.HasText("搜索会话") {
			t.Fatal("narrow window did not collapse project navigation")
		}
		if err := tt.Click("打开历史 one"); err != nil {
			t.Fatal(err)
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
			if tt.HasText("搜索会话") {
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
			if !tt.HasText("搜索会话") {
				t.Fatal("narrow project navigation unreachable")
			}
			if err := tt.Click("切换会话 1"); err != nil {
				t.Fatal(err)
			}
			if tt.HasText("搜索会话") {
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
	a.nativeThreads = []codex.ThreadMeta{
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
		})
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
			a.nativeThreads = []codex.ThreadMeta{{ID: "one", Cwd: "/one", Preview: "历史"}}
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
				project, projectShown := tt.Find("搜索会话")
				if projectShown {
					if left.X+left.W > a.sidebarWidth || left.Y >= project.Y {
						t.Fatalf("left toggle outside panel header: toggle=%+v search=%+v", left, project)
					}
				} else if left.X > 40 || left.Y >= 46 {
					t.Fatalf("collapsed left toggle not in conversation toolbar: %+v", left)
				}
				right, _ := tt.Find("切换右侧边栏")
				history, historyShown := tt.Find("搜索历史")
				directory, _ := tt.Find("工作目录")
				if historyShown {
					if right.X < history.X || right.Y >= history.Y || directory.X+directory.W >= history.X {
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
