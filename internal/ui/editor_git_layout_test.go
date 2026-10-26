package ui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

func TestGitFileRowHoverKeepsGeometryAndActiveComparison(t *testing.T) {
	a, _, root := p4App(t)
	w := a.workspace(root)
	entry := gitview.Entry{Path: "internal/ui/main.go", XY: "MM"}
	w.gitStats = map[string]gitview.Review{gitKey("false", entry.Path): {Added: 12}}
	a.files = map[string]*filePage{"diff": {root: root, path: filepath.Join(root, entry.Path), kind: "diff", compare: "更改"}}
	a.activeTab = "diff"
	if !a.gitFileActive(w, "更改", entry) || a.gitFileActive(w, "已暂存的更改", entry) {
		t.Fatal("active comparison did not distinguish index and working tree")
	}
	for _, width := range []int{260, 360} {
		tt := native.NewTester(func(c *native.Context) {
			native.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
				a.gitFileRow(c.Key("file"), w, "更改", entry, false)
			})
		}, width, 80)
		tt.Move(float32(width-1), 70)
		before, _ := tt.Find("Git 更改 " + entry.Path)
		selected := tt.Image().RGBAAt(2, 14)
		if _, ok := tt.Find("暂存 " + entry.Path); ok {
			t.Fatal("actions visible before hover")
		}
		if _, ok := tt.Find("+12"); !ok {
			t.Fatal("nonzero line count hidden")
		}
		if _, ok := tt.Find("−0"); ok {
			t.Fatal("zero line count visible")
		}
		status, ok := tt.Find("M")
		if !ok || status.X < float32(width-24) {
			t.Fatal("status is not at the trailing edge", status)
		}
		count, _ := tt.Find("+12")
		if count.X+count.W > status.X || status.X-count.X-count.W > 14 {
			t.Fatal("line counts should directly precede the status", count, status)
		}
		tt.Move(before.X+10, before.Y+14)
		for _, label := range []string{"打开文件 ", "撤销改动 ", "暂存 "} {
			r, ok := tt.Find(label + entry.Path)
			if !ok || r.X+r.W > count.X || r.H > 28 {
				t.Fatal("hover action missing or outside row", label, r)
			}
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			if tt.Image().RGBAAt(2, 14) != selected {
				t.Fatal("action hover broke the shared selected background")
			}
		}
		if after, _ := tt.Find("Git 更改 " + entry.Path); after != before {
			t.Fatal("hover moved the filename target", before, after)
		}
		tt.Move(float32(width-1), 70)
		if tt.Image().RGBAAt(2, 14) != selected {
			t.Fatal("selection disappeared after pointer left")
		}
	}
}

func TestGitGroupHoverKeepsCountAndBulkActionPositions(t *testing.T) {
	a, _, root := p4App(t)
	w := a.workspace(root)
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
			a.gitGroup(c, w, "更改", []gitview.Entry{{Path: "main.go", XY: ".M"}}, false)
		})
	}, 260, 100)
	tt.Move(259, 90)
	header, _ := tt.Find("折叠 更改")
	count, ok := tt.Find("1")
	if !ok || count.X < header.X+header.W {
		t.Fatal("group count should be at the right edge", count, header)
	}
	if _, ok := tt.Find("全部暂存"); ok {
		t.Fatal("bulk action visible outside hover")
	}
	tt.Move(header.X+10, header.Y+13)
	action, ok := tt.Find("全部暂存")
	if !ok {
		t.Fatal("bulk action missing on heading hover")
	}
	tt.Move(action.X+action.W/2, action.Y+action.H/2)
	if after, _ := tt.Find("1"); after != count {
		t.Fatal("hover moved group count", count, after)
	}
	if after, _ := tt.Find("折叠 更改"); after != header {
		t.Fatal("hover moved heading", header, after)
	}
}

func TestGitPanelUsesContinuousCompactRows(t *testing.T) {
	for _, width := range []float32{260, 360} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, _, root := p4App(t)
			w := a.workspace(root)
			w.git = gitview.Status{Branch: "main", Upstream: "origin/main", Entries: []gitview.Entry{
				{Path: "internal/ui/first.go", XY: ".M"},
				{Path: "internal/ui/a-long-file-name-to-check-narrow-layout.go", XY: ".M"},
				{Path: "README.md", XY: "M."},
			}}
			w.gitTime, w.gitFingerprint = time.Now(), "fixture"
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Children(func() { a.gitPanel(c, w) })
			}, int(width), 400)
			find := func(label string) native.Rect {
				t.Helper()
				r, ok := tt.Find(label)
				if !ok {
					t.Fatal("missing", label)
				}
				return r
			}
			header := find("折叠 更改")
			first := find("Git 更改 internal/ui/first.go")
			second := find("Git 更改 internal/ui/a-long-file-name-to-check-narrow-layout.go")
			staged := find("折叠 已暂存的更改")
			stagedFile := find("Git 已暂存的更改 README.md")
			if header.H != 26 || first.H != 28 || second.H != 28 ||
				first.Y != header.Y+header.H || second.Y != first.Y+first.H ||
				stagedFile.Y != staged.Y+staged.H || staged.Y != second.Y+second.H+8 {
				t.Fatalf("unexpected row rhythm: header=%+v first=%+v second=%+v staged=%+v", header, first, second, staged)
			}
			if first.X < 0 || first.X+first.W > width || second.X != first.X || second.W != first.W {
				t.Fatal("long path changed row bounds", first, second)
			}
			if err := tt.Click("折叠 更改"); err != nil {
				t.Fatal(err)
			}
			if _, ok := tt.Find("Git 更改 internal/ui/first.go"); ok {
				t.Fatal("folded group still displays files")
			}
			if folded := find("切换最近提交"); folded.Y+folded.H != 396 {
				t.Fatal("history should remain pinned to the bottom", folded)
			}
			if folded := find("折叠 已暂存的更改"); folded.Y != header.Y+header.H+8 {
				t.Fatal("folding left empty file-row space", folded)
			}
		})
	}
}

func TestGitPanelHidesEmptyGroups(t *testing.T) {
	for _, xy := range []string{"", ".M", "M."} {
		t.Run(xy, func(t *testing.T) {
			a, _, root := p4App(t)
			w := a.workspace(root)
			w.gitTime, w.gitFingerprint = time.Now(), "fixture"
			if xy != "" {
				w.git.Entries = []gitview.Entry{{Path: "main.go", XY: xy}}
			}
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Children(func() { a.gitPanel(c, w) })
			}, 360, 400)
			for _, group := range []string{"更改", "已暂存的更改", "冲突"} {
				_, visible := tt.Find("折叠 " + group)
				if visible != (len(gitEntries(w, group)) > 0) || tt.HasText("没有"+group) {
					t.Fatal("empty group or placeholder rendered", group, xy)
				}
			}
			history, _ := tt.Find("切换最近提交")
			if history.Y+history.H != 396 {
				t.Fatal("empty groups moved history away from bottom", history)
			}
		})
	}
}

func TestGitHistoryStaysAtBottomWithIndependentBoundedScroll(t *testing.T) {
	for _, height := range []int{320, 500, 900} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			a, _, root := p4App(t)
			w := a.workspace(root)
			w.gitTime, w.gitFingerprint = time.Now(), "fixture"
			w.gitHistoryLoaded, w.gitHistoryMore = true, true
			for i := range 40 {
				w.git.Entries = append(w.git.Entries, gitview.Entry{Path: fmt.Sprintf("file-%02d.go", i), XY: ".M"})
				w.gitHistory = append(w.gitHistory, gitview.CommitInfo{
					Hash: fmt.Sprintf("%040d", i), Subject: fmt.Sprintf("commit %d", i),
				})
			}
			w.git.Entries = append(w.git.Entries, gitview.Entry{Path: "conflict.go", XY: "UU", Conflict: true})
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Children(func() { a.gitPanel(c, w) })
			}, 360, height)
			find := func(label string) native.Rect {
				t.Helper()
				r, ok := tt.Find(label)
				if !ok {
					t.Fatal("missing", label)
				}
				return r
			}
			header := find("切换最近提交")
			conflict := find("折叠 冲突")
			files := find("折叠 更改")
			if conflict.Y >= files.Y {
				t.Fatal("conflicts should remain first")
			}
			tt.Scroll(files.X+30, files.Y+15, 0, 160)
			if w.gitScroll.Y == 0 || find("切换最近提交") != header {
				t.Fatal("file scroll moved history heading", header, w.gitScroll.Y)
			}
			if err := tt.Click("切换最近提交"); err != nil {
				t.Fatal(err)
			}
			list := find("最近提交列表")
			header = find("切换最近提交")
			contentTop := conflict.Y - 4
			available := float32(height) - contentTop
			if list.H <= 0 || list.H > 260 || list.Y+list.H > float32(height) ||
				header.Y+header.H+list.H > float32(height) ||
				header.Y < float32(height)-available*.45-1 {
				t.Fatal("history exceeded its bounded bottom region", header, list, available)
			}
			if header.Y-contentTop < 28 {
				t.Fatal("history squeezed file list away", header)
			}
			tt.Scroll(files.X+30, contentTop+(header.Y-contentTop)/2, 0, 100)
			if find("切换最近提交") != header {
				t.Fatal("expanded history moved with file scroll")
			}
			fileY := w.gitScroll.Y
			tt.Scroll(list.X+30, list.Y+list.H/2, 0, 200)
			if w.gitHistoryScroll.Y <= 0 || w.gitScroll.Y != fileY || find("切换最近提交") != header {
				t.Fatal("history scroll was not independent", w.gitHistoryScroll.Y, w.gitScroll.Y, fileY)
			}
			tt.Scroll(list.X+30, list.Y+list.H/2, 0, 100000)
			more := find("加载更多提交")
			if more.Y < list.Y || more.Y+more.H > list.Y+list.H+1 {
				t.Fatal("pagination unreachable in history viewport", more, list)
			}
			tt.Click("切换最近提交")
			if collapsed := find("切换最近提交"); collapsed.Y+collapsed.H != float32(height-4) {
				t.Fatal("collapsed history no longer at bottom", collapsed)
			}
		})
	}
}
