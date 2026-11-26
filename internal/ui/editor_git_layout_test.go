package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
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
			if !ok || r.X < before.X+before.W || r.X+r.W > float32(width) || r.H > 28 {
				t.Fatal("hover action missing or outside row", label, r)
			}
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			if tt.Image().RGBAAt(2, 14) != selected {
				t.Fatal("action hover broke the shared selected background")
			}
		}
		if tt.HasText("+12") || tt.HasText("M") {
			t.Fatal("summary remains visible alongside hover actions")
		}
		if after, _ := tt.Find("Git 更改 " + entry.Path); after != before {
			t.Fatal("hover moved the filename target", before, after)
		}
		tt.Move(float32(width-1), 70)
		if tt.Image().RGBAAt(2, 14) != selected {
			t.Fatal("selection disappeared after pointer left")
		}
		if !tt.HasText("+12") || !tt.HasText("M") || tt.HasText("暂存 "+entry.Path) {
			t.Fatal("leaving the row did not restore the summary")
		}
	}
}

func TestGitStagedRowHoverSwapsSummaryForRightAlignedActions(t *testing.T) {
	a, _, root := p4App(t)
	w := a.workspace(root)
	entry := gitview.Entry{Path: "src/components/index.vue", XY: "A."}
	w.gitStats = map[string]gitview.Review{gitKey("true", entry.Path): {Added: 387}}
	for _, width := range []int{260, 360} {
		tt := native.NewTester(func(c *native.Context) {
			native.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
				a.gitFileRow(c.Key("file"), w, "已暂存的更改", entry, false)
			})
		}, width, 80)
		tt.Move(float32(width-1), 70)
		if _, ok := tt.Find("取消暂存 " + entry.Path); ok {
			t.Fatal("actions visible before hover")
		}
		status, ok := tt.Find("A")
		if !ok || status.X < float32(width-24) {
			t.Fatal("status is not at the trailing edge", status)
		}
		count, ok := tt.Find("+387")
		if !ok || count.X+count.W > status.X {
			t.Fatal("line counts should directly precede the status", count, status)
		}
		before, _ := tt.Find("Git 已暂存的更改 " + entry.Path)
		tt.Move(before.X+10, before.Y+14)
		unstage, ok := tt.Find("取消暂存 " + entry.Path)
		if !ok {
			t.Fatal("unstage action missing on hover")
		}
		if unstage.X+unstage.W < float32(width-2) {
			t.Fatal("unstage action is not right-aligned at the row edge", unstage, width)
		}
		open, openOK := tt.Find("打开文件 " + entry.Path)
		if !openOK || open.X < before.X+before.W || open.X+open.W > float32(width) {
			t.Fatal("open action missing or outside row", open)
		}
		if gap := unstage.X - (open.X + open.W); gap > 1 {
			t.Fatal("unexpected gap between hover actions", open, unstage)
		}
		if tt.HasText("+387") || tt.HasText("A") {
			t.Fatal("summary remains visible alongside hover actions")
		}
		if after, _ := tt.Find("Git 已暂存的更改 " + entry.Path); after != before {
			t.Fatal("hover moved the filename target", before, after)
		}
		tt.Move(float32(width-1), 70)
		if !tt.HasText("+387") || tt.HasText("取消暂存 "+entry.Path) {
			t.Fatal("leaving the row did not restore the summary")
		}
	}
}

func TestGitFileRowPrioritizesFilenameBeforeDirectory(t *testing.T) {
	for _, group := range []string{"更改", "已暂存的更改"} {
		for _, width := range []int{240, 320, 520} {
			t.Run(fmt.Sprintf("%s/%d", group, width), func(t *testing.T) {
				a, _, root := p4App(t)
				w := a.workspace(root)
				name, directory := "2026-10-26-git-workflow.md", "docs/verification"
				entry := gitview.Entry{Path: directory + "/" + name, XY: "MM"}
				var nameWidth float32
				tt := native.NewTester(func(c *native.Context) {
					nameWidth, _ = c.MeasureText(0, native.Span{Text: name, Size: 12})
					native.Column(c).Fill().Children(func() { a.gitFileRow(c.Key("file"), w, group, entry, false) })
				}, width, 80)
				tt.Move(float32(width-1), 70)
				filename, _ := tt.Find(name)
				parent, _ := tt.Find(directory)
				if filename.W < nameWidth-.5 && parent.W > .5 {
					t.Fatalf("directory takes space before filename fits: filename=%+v natural=%v parent=%+v", filename, nameWidth, parent)
				}
				tt.Move(10, 14)
				after, _ := tt.Find(name)
				if after != filename {
					t.Fatal("hover moved filename", filename, after)
				}
				if group == "更改" {
					open, _ := tt.Find("打开文件 " + entry.Path)
					row, _ := tt.Find("Git 更改 " + entry.Path)
					if open.X-row.X-row.W > 1 || row.X+row.W-filename.X-filename.W > 8 && filename.W < nameWidth-.5 {
						t.Fatal("truncated filename ends too far from open icon", filename, row, open)
					}
				}
			})
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

func TestGitFileActionsShowOneActionOnlyTooltip(t *testing.T) {
	for _, group := range []string{"更改", "已暂存的更改", "冲突"} {
		t.Run(group, func(t *testing.T) {
			a, _, root := p4App(t)
			w := a.workspace(root)
			entry := gitview.Entry{Path: "internal/ui/main.go", XY: "MM", Conflict: group == "冲突"}
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Children(func() { a.gitFileRow(c.Key("file"), w, group, entry, false) })
			}, 360, 100)
			actions := []string{"打开文件", "暂存"}
			if group == "更改" {
				actions = append(actions, "撤销改动")
			} else if group == "已暂存的更改" {
				actions[1] = "取消暂存"
			} else {
				actions[1] = "标记已解决"
			}
			for _, action := range actions {
				tt.Move(0, 90)
				tt.Move(10, 14)
				before := tt.Texts()
				r, ok := tt.Find(action + " " + entry.Path)
				if !ok {
					t.Fatal("missing action", action)
				}
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				tt.Advance(time.Second)
				after := tt.Texts()
				if len(after) != len(before)+1 || !slices.Contains(after, action) {
					t.Fatalf("expected one action-only tooltip for %s: before=%v after=%v", action, before, after)
				}
			}
		})
	}
}

func TestGitWorkingTreeTrailingAreaSwapsLargeCountsAndKeyboardActions(t *testing.T) {
	a, _, root := p4App(t)
	w := a.workspace(root)
	entry := gitview.Entry{Path: "main.go", XY: ".M"}
	w.gitStats = map[string]gitview.Review{gitKey("false", entry.Path): {Added: 1234567, Deleted: 7654321}}
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() { a.gitFileRow(c.Key("file"), w, "更改", entry, true) })
	}, 260, 80)
	tt.Move(259, 70)
	before, _ := tt.Find("Git 更改 " + entry.Path)
	for _, label := range []string{"+1234567", "−7654321", "M"} {
		r, ok := tt.Find(label)
		if !ok || r.X < before.X+before.W || r.X+r.W > 260 {
			t.Fatal("large summary outside shared trailing area", label, r)
		}
	}
	tt.Key(0, native.KeyTab)
	if !tt.HasText("打开文件 "+entry.Path) || tt.HasText("+1234567") || tt.HasText("M") {
		t.Fatal("keyboard focus did not replace summary with actions")
	}
	if after, _ := tt.Find("Git 更改 " + entry.Path); after != before {
		t.Fatal("large counts and actions reserve different widths")
	}
	tt.Move(259, 70)
	if !tt.HasText("打开文件 " + entry.Path) {
		t.Fatal("moving pointer away hid keyboard actions")
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
			tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
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
			if heading := find("切换 COMMITS"); heading.Y < staged.Y {
				t.Fatal("COMMITS should follow change groups", heading)
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
			if history, ok := tt.Find("切换 COMMITS"); !ok || history.H != 22 {
				t.Fatal("COMMITS heading missing", history)
			}
		})
	}
}

func TestGitCommitsBottomPaneResizesAndScrollsIndependently(t *testing.T) {
	for _, height := range []int{320, 500, 900} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			a, _, root := p4App(t)
			w := a.workspace(root)
			w.gitTime, w.gitFingerprint = time.Now(), "fixture"
			w.gitHistoryLoaded = true
			w.git.Entries = []gitview.Entry{{Path: "modified.go", XY: ".M"}}
			for i := range 100 {
				w.gitHistory = append(w.gitHistory, gitview.CommitInfo{Hash: fmt.Sprintf("%040d", i), Subject: fmt.Sprintf("commit %d", i)})
			}
			tt := native.NewTester(func(c *native.Context) { native.Column(c).Fill().Children(func() { a.gitPanel(c, w) }) }, 360, height)
			if !tt.HasText("COMMITS") {
				t.Fatal("COMMITS missing")
			}
			list, ok := tt.Find("COMMITS 列表")
			if !ok || list.Y+list.H > float32(height)+1 || list.H < 40 {
				t.Fatal("commits pane bounds", list)
			}
			if _, ok := tt.Find("提交 " + w.gitHistory[99].Hash); ok {
				t.Fatal("virtual list built offscreen history")
			}
			tt.Scroll(list.X+30, list.Y+list.H/2, 0, 100000)
			if _, ok := tt.Find("提交 " + w.gitHistory[99].Hash); !ok {
				t.Fatal("oldest commit unreachable")
			}
			if _, ok := tt.Find("Git 更改 modified.go"); !ok {
				t.Fatal("scrolling commits moved the change rows")
			}
			if w.gitHistoryLoading {
				t.Fatal("scroll triggered automatic pagination")
			}
			divider, ok := tt.Find("Divider")
			if !ok || divider.W < 300 || divider.H != 1 {
				t.Fatal("missing horizontal divider above commits", divider)
			}
			// Blank space in the changes pane cannot select a virtual row.
			changes, _ := tt.Find("Git 更改列表")
			selected := w.gitListRow
			x, y := changes.X+4, changes.Y+changes.H-8
			before := tt.Image().RGBAAt(int(x), int(y))
			tt.Press(x, y)
			tt.Release(x, y)
			tt.Move(359, 0)
			if w.gitListRow != selected || tt.Image().RGBAAt(int(x), int(y)) != before {
				t.Fatal("blank changes area acquired a selection background")
			}
			header, _ := tt.Find("切换 COMMITS")
			x, y = divider.X+divider.W/2, divider.Y+divider.H/2
			tt.Move(x, y)
			if tt.Cursor() != native.CursorResizeNS {
				t.Fatal("commits divider has no vertical resize cursor")
			}
			tt.Press(x, y)
			tt.Move(x, y-40)
			tt.Release(x, y-40)
			tt.Frame()
			raised, _ := tt.Find("切换 COMMITS")
			if raised.Y >= header.Y-10 {
				t.Fatal("dragging up did not grow commits", header, raised)
			}
			tt.Key(0, native.KeyDown)
			lowered, _ := tt.Find("切换 COMMITS")
			if lowered.Y <= raised.Y {
				t.Fatal("keyboard resize did not lower commits", raised, lowered)
			}
			size := w.gitChangesPercent
			if err := tt.Click("切换 COMMITS"); err != nil {
				t.Fatal(err)
			}
			folded, _ := tt.Find("切换 COMMITS")
			if folded.Y+folded.H < float32(height)-1 {
				t.Fatal("collapsed commits is not at panel bottom", folded)
			}
			if err := tt.Click("切换 COMMITS"); err != nil {
				t.Fatal(err)
			}
			reopened, _ := tt.Find("切换 COMMITS")
			if w.gitChangesPercent != size || reopened.Y != lowered.Y {
				t.Fatal("reopening reset commits height", reopened, lowered)
			}
		})
	}
}
