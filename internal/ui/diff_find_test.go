package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
	"github.com/shenming0821/Sasa/internal/storage"
)

func newTestDiffPage(t *testing.T, split bool, beforeText, patch string) (*App, chan func(), *native.Tester, *filePage) {
	t.Helper()
	a, q, root := p4App(t)
	path := filepath.Join(root, "sample.go")
	review := gitview.ParsePatch(patch)
	review.BeforeKnown, review.BeforeText = beforeText != "", beforeText
	review.Entry.Path = "sample.go"
	d := &filePage{
		id:           "diff-sample",
		root:         root,
		path:         path,
		kind:         "diff",
		compare:      "更改",
		gitDiffState: gitDiffState{gitSplit: split},
	}
	a.gitSetDiffText(d, patch, &review)
	if a.files == nil {
		a.files = map[string]*filePage{}
	}
	a.files[d.id] = d
	a.tabs = append(a.tabs, storage.Tab{ID: d.id, Kind: "diff", Workspace: root, Path: path, Compare: "更改"})
	a.selectFile(d)
	tt := native.NewTester(a.view, 1200, 760)
	for range 3 {
		tt.Frame()
	}
	return a, q, tt, d
}

func TestDiffInlineCmdFFindStepsAndEscapes(t *testing.T) {
	before := "alpha one\nold_only line\nalpha two\nAlpha three\n"
	patch := "diff --git a/sample.go b/sample.go\n--- a/sample.go\n+++ b/sample.go\n@@ -1,4 +1,4 @@\n alpha one\n-old_only line\n+new_only line\n alpha two\n Alpha three\n"
	_, q, tt, d := newTestDiffPage(t, false, before, patch)

	if !tt.Focused("文件内容 " + d.path) {
		if err := tt.Click("文件内容 " + d.path); err != nil {
			t.Fatal(err)
		}
	}
	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	if !d.findOpen || !tt.Focused("文件内查找") {
		t.Fatalf("Cmd+F did not open and focus find bar on inline diff: open=%v focused=%v", d.findOpen, tt.Focused("文件内查找"))
	}

	tt.Type("alpha")
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.buffer && d.findResult == "1/3" })
	if got := d.buffer.Slice(min(d.anchor, d.caret), max(d.anchor, d.caret)); !strings.EqualFold(got, "alpha") {
		t.Fatalf("unexpected inline match selection: %q", got)
	}

	// Enter / Shift+Enter step and wrap across matches.
	tt.Key(0, native.KeyEnter)
	tt.Frame()
	if d.findIndex != 1 || d.findResult != "2/3" {
		t.Fatalf("Enter did not advance inline diff match: index=%d result=%q", d.findIndex, d.findResult)
	}
	tt.Key(native.Shift, native.KeyEnter)
	tt.Frame()
	if d.findIndex != 0 || d.findResult != "1/3" {
		t.Fatalf("Shift+Enter did not move back in inline diff: index=%d result=%q", d.findIndex, d.findResult)
	}
	if err := tt.Click("区分大小写"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		return d.findScanned && d.findCase && d.findResult == "2/2" || (d.findScanned && d.findCase && len(d.findMatches) == 2)
	})
	if len(d.findMatches) != 2 {
		t.Fatalf("case-sensitive match count mismatch: %d", len(d.findMatches))
	}

	// Escape closes find and returns focus to the inline diff area.
	tt.Key(0, native.KeyEscape)
	tt.Frame()
	if d.findOpen || !tt.Focused("文件内容 "+d.path) {
		t.Fatalf("Escape did not close inline diff find and restore focus: open=%v focused=%v", d.findOpen, tt.Focused("文件内容 "+d.path))
	}

	// Cmd+F also works when focus is outside the diff text area (e.g. on a toolbar button).
	if err := tt.Click("下一个修改"); err != nil {
		t.Fatal(err)
	}
	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	if !d.findOpen || !tt.Focused("文件内查找") {
		t.Fatalf("Cmd+F from diff toolbar did not open find bar: open=%v focused=%v", d.findOpen, tt.Focused("文件内查找"))
	}
}

func TestDiffSplitCmdFSearchesFocusedSideOnly(t *testing.T) {
	before := "alpha first\nold_only alpha\nshared line\nold_only tail\nalpha last\n"
	patch := "diff --git a/sample.go b/sample.go\n--- a/sample.go\n+++ b/sample.go\n@@ -1,5 +1,5 @@\n alpha first\n-old_only alpha\n+new_only item\n shared line\n-old_only tail\n-alpha last\n+new_only tail\n+beta last\n"
	_, q, tt, d := newTestDiffPage(t, true, before, patch)

	if !d.gitSplitShown || d.gitLeft == nil || d.gitRight == nil {
		t.Fatalf("split diff not active: shown=%v left=%v right=%v", d.gitSplitShown, d.gitLeft != nil, d.gitRight != nil)
	}

	// 1. Focus left side ("原版本") and search "alpha" -> 3 matches in left only.
	if err := tt.Click("差异 原版本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rightAnchorBefore, rightCaretBefore := d.gitSplitAnchor[1], d.gitSplitCaret[1]
	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	if !d.findOpen || !tt.Focused("文件内查找") {
		t.Fatalf("Cmd+F on left split side did not open/focus find: open=%v focused=%v", d.findOpen, tt.Focused("文件内查找"))
	}
	if !tt.HasText("原版本") {
		t.Fatal("missing left scope label in split find bar")
	}
	tt.Type("alpha")
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.gitLeft && len(d.findMatches) == 3 })
	if d.findResult != "1/3" {
		t.Fatalf("expected 1/3 on left side, got %q", d.findResult)
	}
	if got := d.gitLeft.Slice(min(d.gitSplitAnchor[0], d.gitSplitCaret[0]), max(d.gitSplitAnchor[0], d.gitSplitCaret[0])); got != "alpha" {
		t.Fatalf("left side selection did not match 'alpha': %q", got)
	}
	if d.gitSplitAnchor[1] != rightAnchorBefore || d.gitSplitCaret[1] != rightCaretBefore {
		t.Fatalf("searching left side unexpectedly modified right side selection: got %d..%d want %d..%d", d.gitSplitAnchor[1], d.gitSplitCaret[1], rightAnchorBefore, rightCaretBefore)
	}

	// Stepping advances strictly within the left buffer.
	tt.Key(0, native.KeyEnter)
	tt.Frame()
	if d.findIndex != 1 || d.findResult != "2/3" || d.findBuffer != d.gitLeft {
		t.Fatalf("Enter did not step within left side: index=%d result=%q", d.findIndex, d.findResult)
	}

	// Searching text that only exists on the right yields no match while left is targeted.
	d.query = "new_only"
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.gitLeft && d.findResult == "没有匹配" })

	// 2. Clicking the right side ("新版本") switches search target to the right buffer automatically.
	if err := tt.Click("差异 新版本"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		return d.gitCursorSide == 1 && d.findScanned && d.findBuffer == d.gitRight && len(d.findMatches) == 2
	})
	if !tt.Focused("差异 新版本") {
		t.Fatal("clicking right side lost focus during passive rescan")
	}
	// Clicking next reveals a match on the right side without touching the left side.
	if err := tt.Click("下一个匹配"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if d.findBuffer != d.gitRight || (d.findResult != "1/2" && d.findResult != "2/2") {
		t.Fatalf("stepping after focusing right side did not reveal right match: result=%q", d.findResult)
	}
	if got := d.gitRight.Slice(min(d.gitSplitAnchor[1], d.gitSplitCaret[1]), max(d.gitSplitAnchor[1], d.gitSplitCaret[1])); got != "new_only" {
		t.Fatalf("right side selection did not match 'new_only': %q", got)
	}

	// 3. Focusing right side and pressing Cmd+F searches right side only ("alpha" appears once on right).
	if err := tt.Click("差异 新版本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	if !tt.Focused("文件内查找") {
		t.Fatal("Cmd+F from right side did not focus find input")
	}
	tt.Type("alpha")
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.gitRight && d.findResult == "1/1" })

	// Text unique to the left side is not matched when right side is focused.
	d.query = "old_only"
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.gitRight && d.findResult == "没有匹配" })

	// 4. Escape restores focus to the right side that was searched.
	tt.Key(0, native.KeyEscape)
	tt.Frame()
	if d.findOpen || !tt.Focused("差异 新版本") {
		t.Fatalf("Escape did not restore focus to right split side: open=%v focused=%v", d.findOpen, tt.Focused("差异 新版本"))
	}

	// 5. Focusing left side, opening find, and pressing Escape restores focus to left side.
	if err := tt.Click("差异 原版本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	tt.Key(0, native.KeyEscape)
	tt.Frame()
	if d.findOpen || !tt.Focused("差异 原版本") {
		t.Fatalf("Escape did not restore focus to left split side: open=%v focused=%v", d.findOpen, tt.Focused("差异 原版本"))
	}
}

func TestDiffSplitFindSeedsSelectedWordFromFocusedSide(t *testing.T) {
	before := "left_token keep\nshared\n"
	patch := "diff --git a/sample.go b/sample.go\n--- a/sample.go\n+++ b/sample.go\n@@ -1,2 +1,2 @@\n-left_token keep\n+right_token keep\n shared\n"
	_, q, tt, d := newTestDiffPage(t, true, before, patch)

	// Seed from left side selection.
	if err := tt.Click("差异 原版本"); err != nil {
		t.Fatal(err)
	}
	leftText := d.gitLeft.String()
	leftStart := strings.Index(leftText, "left_token")
	d.gitSplitPendingSelection = &[2]int{leftStart, leftStart + len("left_token")}
	d.gitSplitPendingSide = 0
	tt.Frame()
	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	if !d.findOpen || d.query != "left_token" {
		t.Fatalf("Cmd+F did not seed left selection: open=%v query=%q", d.findOpen, d.query)
	}
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.gitLeft && d.findResult == "1/1" })

	tt.Key(0, native.KeyEscape)
	tt.Frame()

	// Seed from right side selection.
	if err := tt.Click("差异 新版本"); err != nil {
		t.Fatal(err)
	}
	rightText := d.gitRight.String()
	rightStart := strings.Index(rightText, "right_token")
	d.gitSplitPendingSelection = &[2]int{rightStart, rightStart + len("right_token")}
	d.gitSplitPendingSide = 1
	tt.Frame()
	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	if !d.findOpen || d.query != "right_token" {
		t.Fatalf("Cmd+F did not seed right selection: open=%v query=%q", d.findOpen, d.query)
	}
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.gitRight && d.findResult == "1/1" })
}

func TestDiffSplitFindScrollsBothSidesInSync(t *testing.T) {
	var before strings.Builder
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&before, "line %03d filler\n", i)
	}
	patch := "diff --git a/sample.go b/sample.go\n--- a/sample.go\n+++ b/sample.go\n@@ -1,3 +1,3 @@\n-line 001 filler\n+line 001 top_change\n line 002 filler\n line 003 filler\n@@ -115,4 +115,4 @@\n line 115 filler\n-line 116 filler\n+line 116 far_right_target\n line 117 filler\n line 118 filler\n"
	a, q, tt, d := newTestDiffPage(t, true, before.String(), patch)
	expandAllTestDiffContext(a, d)
	for range 3 {
		tt.Frame()
	}
	d.gitSplitY, d.gitLeftScroll.Y, d.gitRightScroll.Y = 0, 0, 0
	tt.Frame()

	if err := tt.Click("差异 新版本"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	tt.Type("far_right_target")
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.gitRight && d.findResult == "1/1" })
	for range 3 {
		tt.Frame()
	}
	if d.gitSplitY <= 0 {
		t.Fatalf("expected split diff to scroll to far match, got gitSplitY=%v", d.gitSplitY)
	}
	if d.gitLeftScroll.Y != d.gitRightScroll.Y || d.gitLeftScroll.Y != d.gitSplitY {
		t.Fatalf("split scroll positions diverged: splitY=%v left=%v right=%v", d.gitSplitY, d.gitLeftScroll.Y, d.gitRightScroll.Y)
	}
}

func TestDiffFindSwitchesBetweenInlineAndSplit(t *testing.T) {
	before := "alpha one\nold alpha\n"
	patch := "diff --git a/sample.go b/sample.go\n--- a/sample.go\n+++ b/sample.go\n@@ -1,2 +1,2 @@\n alpha one\n-old alpha\n+new value\n"
	_, q, tt, d := newTestDiffPage(t, false, before, patch)

	tt.Key(native.Cmd, native.KeyF)
	tt.Frame()
	tt.Type("alpha")
	p4Wait(t, tt, q, func() bool { return d.findScanned && d.findBuffer == d.buffer && d.findResult == "1/2" })

	// Switch to split view while find bar is open: rescans left side (2 matches).
	if err := tt.Click("并排"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		return d.gitSplitShown && d.findScanned && d.findBuffer == d.gitLeft && d.findResult == "1/2"
	})

	// Focus right side where "alpha" only appears once, then switch back to inline (2 matches).
	if err := tt.Click("差异 新版本"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		return d.gitCursorSide == 1 && d.findScanned && d.findBuffer == d.gitRight && len(d.findMatches) == 1
	})

	if err := tt.Click("行内"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		return !d.gitSplitShown && d.findScanned && d.findBuffer == d.buffer && d.findResult == "1/2"
	})
}
