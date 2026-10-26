package ui

import (
	"fmt"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

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
			if header.H != 26 || first.H != 28 || second.H != 28 ||
				first.Y != header.Y+header.H || second.Y != first.Y+first.H ||
				staged.Y != second.Y+second.H+8 {
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
			if folded := find("折叠 已暂存的更改"); folded.Y != header.Y+header.H+8 {
				t.Fatal("folding left empty file-row space", folded)
			}
		})
	}
}
