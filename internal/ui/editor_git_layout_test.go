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
				stagedFile.Y != staged.Y+staged.H || header.Y != stagedFile.Y+stagedFile.H+8 {
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
			if folded := find("切换最近提交"); folded.Y != header.Y+header.H+8 {
				t.Fatal("folding left empty file-row space", folded)
			}
		})
	}
}
