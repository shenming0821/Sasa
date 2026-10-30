package ui

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

func changeMessage(id, turn, path, patch string) *app.Message {
	return &app.Message{ID: id, Role: "tool", Done: true, TurnID: turn, Tool: &app.Tool{Kind: "fileChange", Status: "completed", Changes: []app.FileChange{{Path: path, Kind: json.RawMessage(`{"type":"update"}`), Diff: patch}}}}
}
func cardFixture() []*app.Message {
	var result []*app.Message
	for i := 0; i < 5; i++ {
		result = append(result, changeMessage(fmt.Sprint(i), "turn", fmt.Sprintf("/workspace/internal/ui/file_%d.go", i), "@@ -1 +1,2 @@\n-old\n+new\n+added\n"))
	}
	return result
}

func TestChangeSummaryTurnPlacementAndAnchors(t *testing.T) {
	a := New(nil, "test", "/workspace", nil)
	one := changeMessage("one", "first", "a.go", "@@ -1 +1 @@\n-old\n+new\n")
	two := changeMessage("two", "first", "a.go", "@@ -1 +1 @@\n-new\n+final\n")
	bad := changeMessage("bad", "first", "failed.go", "")
	bad.Tool.Status = "failed"
	next := changeMessage("next", "second", "b.go", "@@ -1 +1 @@\n-b\n+c\n")
	a.State.Messages = []*app.Message{one, {ID: "explain", Role: "assistant", Text: "progress", TurnID: "first"}, two, bad, {ID: "answer", Role: "assistant", Text: "done", Done: true, TurnID: "first"}, {ID: "user", Role: "user", Text: "next"}, next, {ID: "answer2", Role: "assistant", Text: "done2", Done: true, TurnID: "second"}}
	rows := a.conversationRows()
	if len(rows) != 7 || rows[1].message != bad || rows[2].message.ID != "answer" || len(rows[3].changes) != 2 || rows[5].message.ID != "answer2" || len(rows[6].changes) != 1 {
		t.Fatalf("turn projection=%+v", rows)
	}
	a.saved.Anchor = "two"
	a.saved.AnchorOffset = 30
	a.restoreReading()
	if a.saved.Anchor != "one" || a.saved.AnchorOffset != 0 {
		t.Fatal("grouped file anchor not restored")
	}
	summary := a.summarizeChanges(rows[3].changes)
	if len(summary.Files) != 1 || summary.Added != 1 || summary.Deleted != 1 {
		t.Fatal("duplicate edits counted", summary)
	}
	two.Tool.Changes[0].Diff = "@@ -1 +1 @@\n-new\n+old\n"
	summary = a.summarizeChanges(rows[3].changes)
	if summary.Added != 0 || summary.Deleted != 0 {
		t.Fatal("cached stats did not refresh after protocol update")
	}
}

func TestChangeCardResponsiveExpansionAndHistoricReview(t *testing.T) {
	for _, width := range []int{320, 700} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/dark=%v", width, dark), func(t *testing.T) {
				a := New(nil, "test", "/workspace", nil)
				messages := cardFixture()
				tt := native.NewTester(func(c *native.Context) { native.Column(c).Fill().Children(func() { a.changeCard(c, messages) }) }, width, 650)
				tt.SetDark(dark)
				tt.Advance(200 * time.Millisecond)
				if dir := os.Getenv("FUYU_CHANGE_PREVIEW"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("change-card-%d-dark-%v.png", width, dark)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				if !tt.HasText("已编辑 5 个文件") || !tt.HasText("+10") || !tt.HasText("−5") || !tt.HasText("再显示 2 个文件") || tt.HasText("file_3.go") {
					t.Fatal("summary not compact by default", tt.Texts())
				}
				for _, text := range tt.Texts() {
					if strings.Contains(text, "/workspace/") && !strings.HasPrefix(text, "查看本轮差异 ") {
						t.Fatal("absolute root leaked into visible file text", text)
					}
				}
				for i := 0; i < 3; i++ {
					label := fmt.Sprintf("查看本轮差异 /workspace/internal/ui/file_%d.go", i)
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.X+r.W > float32(width) || r.H < 30 {
						t.Fatalf("file row clipped: %+v", r)
					}
				}
				if err := tt.Click("再显示 2 个文件"); err != nil {
					t.Fatal(err)
				}
				if !tt.HasText("file_4.go") || !tt.HasText("收起文件列表") {
					t.Fatal("remaining files inaccessible")
				}
				if err := tt.Click("收起文件列表"); err != nil {
					t.Fatal(err)
				}
				if tt.HasText("file_4.go") {
					t.Fatal("collapse retained hidden rows")
				}
				if err := tt.Click("查看本轮差异 /workspace/internal/ui/file_1.go"); err != nil {
					t.Fatal(err)
				}
				d := a.currentFile()
				if d == nil || d.turnReview == nil || d.turnReview.selected != 1 || !strings.Contains(d.buffer.String(), "added") || d.gitReview.Blockable {
					t.Fatal("file did not open immutable protocol diff")
				}
				a.gitStepFile(d, 1)
				if d.turnReview.selected != 2 || !strings.HasSuffix(d.path, "file_2.go") {
					t.Fatal("historical file navigation failed")
				}
				snapshot := a.snapshot()
				for _, tab := range snapshot.WorkspaceTabs {
					if tab.ID == d.id {
						t.Fatal("transient protocol diff persisted as live Git comparison")
					}
				}
				if snapshot.ActiveTabID != sessionTabID(a.session.id) {
					t.Fatal("restart fallback did not preserve conversation")
				}
			})
		}
	}
}

func TestChangeCardReviewSurvivesGitRefresh(t *testing.T) {
	repo := projectFixture(t)
	path := filepath.Join(repo.Path, "test.go")
	if err := os.WriteFile(path, []byte("unrelated working tree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := New(nil, "test", repo.Path, nil)
	messages := []*app.Message{changeMessage("one", "turn", path, "@@ -1 +1 @@\n-old\n+historical\n")}
	a.openTurnReview(messages, app.SummarizeChanges(a.State.Cwd, messages), 0)
	d := a.currentFile()
	before := d.buffer.String()
	queue := make(chan func(), 20)
	a.dispatch = func(fn func()) { queue <- fn }
	a.refreshGit(a.workspace(repo.Path))
	pumpUpdate(t, queue)
	if d.buffer.String() != before || d.err != "" || !strings.Contains(before, "historical") {
		t.Fatal("live refresh overwrote historic diff", d.err)
	}
	// A cache eviction must reconstruct from the retained review, not current disk.
	d.loaded = false
	d.buffer = nil
	a.loadFile(d)
	if d.buffer.String() != before {
		t.Fatal("review cache reload used live file")
	}
	tt := native.NewTester(func(c *native.Context) {
		native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
	}, 900, 600)
	if !tt.HasText("本轮修改的文件") || tt.HasText("暂存此修改块") {
		t.Fatal("historic review exposed working-tree write action")
	}
}

func TestChangeSummaryIncompleteDisplayAndLongPaths(t *testing.T) {
	a := New(nil, "test", "/workspace", nil)
	path := "/workspace/" + strings.Repeat("long-directory/", 12) + strings.Repeat("中文文件名", 15) + ".go"
	m := changeMessage("file", "turn", path, "@@ -1 +1 @@\n-old\n+new\n")
	a.State.Messages = []*app.Message{m}
	a.State.TurnID = "turn"
	a.State.DisplayTrimmed = true
	if got := a.summarizeChanges([]*app.Message{m}); got.Exact {
		t.Fatal("evicted turn opening shown as complete")
	}
	a.State.DisplayTrimmed = false
	a.State.Incomplete = true
	if got := a.summarizeChanges([]*app.Message{m}); got.Exact {
		t.Fatal("protocol gap shown as complete")
	}
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() { a.changeCard(c, []*app.Message{m}) })
	}, 700, 400)
	if !tt.HasText("统计不完整") || tt.HasText("+1") {
		t.Fatal("unknown totals displayed as numbers")
	}
	for _, width := range []int{320, 700, 320} {
		tt.SetSize(width, 400)
		tt.Advance(200 * time.Millisecond)
		r, ok := tt.Find("查看本轮差异 " + path)
		if !ok || r.X+r.W > float32(width) || r.H > 60 {
			t.Fatal("long file path enlarged summary", r)
		}
	}
}
