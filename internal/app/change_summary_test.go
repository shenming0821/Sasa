package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func fileChangeMessage(path, kind, diff string) *Message {
	return &Message{ID: path, Role: "tool", TurnID: "turn", Tool: &Tool{Kind: "fileChange", Status: "completed", Changes: []FileChange{{Path: path, Kind: json.RawMessage(kind), Diff: diff}}}}
}

func TestTurnChangesComposeNetEdits(t *testing.T) {
	update := `{"type":"update"}`
	region := `{"type":"update","region":true}`
	tests := []struct {
		name     string
		changes  []*Message
		add, del int
		exact    bool
	}{
		{"repeated replacement", []*Message{fileChangeMessage("a.go", update, "@@ -1 +1 @@\n-old\n+middle\n"), fileChangeMessage("a.go", update, "@@ -1 +1 @@\n-middle\n+new\n")}, 1, 1, true},
		{"reverted edit", []*Message{fileChangeMessage("a.go", update, "@@ -1 +1 @@\n-old\n+middle\n"), fileChangeMessage("a.go", update, "@@ -1 +1 @@\n-middle\n+old\n")}, 0, 0, true},
		{"insert then edit", []*Message{fileChangeMessage("a.go", update, "@@ -0,0 +1,2 @@\n+one\n+two\n"), fileChangeMessage("a.go", update, "@@ -1,2 +1,2 @@\n one\n-two\n+changed\n")}, 2, 0, true},
		{"insert then remove", []*Message{fileChangeMessage("a.go", update, "@@ -0,0 +1 @@\n+one\n"), fileChangeMessage("a.go", update, "@@ -1 +0,0 @@\n-one\n")}, 0, 0, true},
		{"context and separate hunks", []*Message{fileChangeMessage("a.go", update, "@@ -2,2 +2,3 @@\n context\n-old\n+new\n+added\n@@ -10 +11 @@\n-removed\n+final\n")}, 3, 2, true},
		{"LCS can realign known context", []*Message{fileChangeMessage("a.go", update, "@@ -1,3 +1,3 @@\n-a\n-a\n b\n+a\n+a\n")}, 1, 1, true},
		{"LCS preserves equal middle", []*Message{fileChangeMessage("a.go", update, "@@ -1,3 +1,3 @@\n-first\n-keep\n-last\n+new\n+keep\n+end\n")}, 2, 2, true},
		{"new file edited", []*Message{fileChangeMessage("a.go", `{"type":"add"}`, "one\ntwo\n"), fileChangeMessage("a.go", update, "@@ -2 +2 @@\n-two\n+three\n")}, 2, 0, true},
		{"new file deleted", []*Message{fileChangeMessage("a.go", `{"type":"add"}`, "one\ntwo\n"), fileChangeMessage("a.go", `{"type":"delete"}`, "one\ntwo\n")}, 0, 0, true},
		{"deleted file recreated", []*Message{fileChangeMessage("a.go", `{"type":"delete"}`, "one\ntwo\n"), fileChangeMessage("a.go", `{"type":"add"}`, "one\nthree\n")}, 1, 1, true},
		{"delete unified", []*Message{fileChangeMessage("a.go", `{"type":"delete"}`, "@@ -1,2 +0,0 @@\n-one\n-two\n")}, 0, 2, true},
		{"newline only", []*Message{fileChangeMessage("a.go", update, "@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+a\n")}, 1, 1, true},
		{"missing patch", []*Message{fileChangeMessage("a.go", update, "")}, 0, 0, false},
		{"malformed patch", []*Message{fileChangeMessage("a.go", update, "@@ -1,2 +1 @@\n-old\n+new\n")}, 0, 0, false},
		{"inconsistent coordinates", []*Message{fileChangeMessage("a.go", update, "@@ -1 +5 @@\n-old\n+new\n")}, 0, 0, false},
		{"conflicting history", []*Message{fileChangeMessage("a.go", update, "@@ -1 +1 @@\n-old\n+middle\n"), fileChangeMessage("a.go", update, "@@ -1 +1 @@\n-unrelated\n+new\n")}, 0, 0, false},
		{"bounded sparse allocation", []*Message{fileChangeMessage("a.go", update, "@@ -999999999 +999999999 @@\n-old\n+new\n")}, 0, 0, false},
		{"region replacement", []*Message{fileChangeMessage("a.go", region, "-old\n-middle\n+new\n")}, 1, 2, true},
		{"region dedupes kept lines", []*Message{fileChangeMessage("a.go", region, "-same\n-old\n+same\n+new\n")}, 1, 1, true},
		{"region blank lines", []*Message{fileChangeMessage("a.go", region, "-old\n-\n+new\n+\n")}, 1, 1, true},
		{"region new file", []*Message{fileChangeMessage("a.go", `{"type":"add","region":true}`, "+one\n+two\n")}, 2, 0, true},
		{"region delete", []*Message{fileChangeMessage("a.go", `{"type":"delete","region":true}`, "-one\n-two\n")}, 0, 2, true},
		{"second region stays unknown", []*Message{fileChangeMessage("a.go", region, "-a\n+b\n"), fileChangeMessage("a.go", region, "-b\n+c\n")}, 0, 0, false},
		{"empty region is unknown", []*Message{fileChangeMessage("a.go", region, "")}, 0, 0, false},
		{"malformed region", []*Message{fileChangeMessage("a.go", region, "-old\n+new\nnot prefixed\n")}, 0, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SummarizeChanges("", tc.changes)
			if len(got.Files) != 1 || got.Exact != tc.exact || got.Added != tc.add || got.Deleted != tc.del {
				t.Fatalf("summary=%+v", got)
			}
			if tc.exact && tc.add+tc.del > 0 && !strings.Contains(got.Files[0].Patch, "@@") {
				t.Fatal("net diff missing")
			}
		})
	}
}

func TestTurnChangesRenameAndIncompleteInputs(t *testing.T) {
	first := fileChangeMessage("old.go", `{"type":"update","move_path":"new.go"}`, "@@ -1 +1 @@\n-old\n+middle\n")
	second := fileChangeMessage("new.go", `{"type":"update"}`, "@@ -1 +1 @@\n-middle\n+final\n")
	failed := fileChangeMessage("failed.go", `{"type":"add"}`, "failed\n")
	failed.Tool.Status = "failed"
	declined := fileChangeMessage("denied.go", `{"type":"add"}`, "denied\n")
	declined.Tool.Status = "declined"
	got := SummarizeChanges("", []*Message{first, second, failed, declined})
	if len(got.Files) != 1 || got.Files[0].Path != "new.go" || got.Files[0].PreviousPath != "old.go" || got.Added != 1 || got.Deleted != 1 {
		t.Fatalf("rename summary=%+v", got)
	}
	second.Tool.Truncated = true
	got = SummarizeChanges("", []*Message{first, second})
	if got.Exact || got.Files[0].Exact || !strings.Contains(got.Files[0].Patch, "middle") {
		t.Fatal("incomplete data presented as exact")
	}
}

func TestTurnChangesPatchPreservesCoordinates(t *testing.T) {
	// Compose a full-file replacement, a shifted tail edit, and a later insertion.
	old := "a\nb\nc\nd\ne\n"
	changes := []*Message{
		fileChangeMessage("a.go", `{"type":"update"}`, "@@ -1,5 +1,6 @@\n a\n-b\n+new\n+extra\n c\n d\n e\n"),
		fileChangeMessage("a.go", `{"type":"update"}`, "@@ -5,2 +5,2 @@\n-d\n+tail\n e\n"),
		fileChangeMessage("a.go", `{"type":"update"}`, "@@ -6,0 +7 @@\n+last\n"),
	}
	got := SummarizeChanges("", changes)
	if !got.Exact || got.Added != 4 || got.Deleted != 2 {
		t.Fatalf("summary=%+v", got)
	}
	f := &patchFile{exact: true, complete: true}
	for i, s := range rawLines(old) {
		line := patchLine{original: i, text: s, known: true}
		f.base = append(f.base, line)
		f.current = append(f.current, line)
		f.next++
	}
	if !f.apply(got.Files[0].Patch, "update", false) {
		t.Fatal("composed patch not applicable:", got.Files[0].Patch)
	}
	var text strings.Builder
	for _, line := range f.current {
		fmt.Fprintln(&text, line.text)
	}
	if text.String() != "a\nnew\nextra\nc\ntail\ne\nlast\n" {
		t.Fatal("composed patch changed result", text.String())
	}
}

func TestTurnChangesCommitStyleContext(t *testing.T) {
	change := fileChangeMessage("a.go", `{"type":"update"}`,
		"@@ -10,18 +10,19 @@\n a\n b\n c\n d\n-old\n+new\n+extra\n e\n f\n g\n h\n i\n j\n k\n l\n-last\n+final\n m\n n\n o\n p\n")
	summary := SummarizeChanges("", []*Message{change})
	want := "--- a/a.go\n+++ b/a.go\n@@ -11,7 +11,8 @@\n b\n c\n d\n-old\n+new\n+extra\n e\n f\n g\n@@ -20,7 +21,7 @@\n j\n k\n l\n-last\n+final\n m\n n\n o\n"
	if got := summary.Files[0].Patch; got != want {
		t.Fatalf("got patch:\n%s\nwant:\n%s", got, want)
	}
}

func TestTurnChangesNormalizePathsAndKeepUnknownStats(t *testing.T) {
	messages := []*Message{
		fileChangeMessage("a.go", `{"type":"update"}`, "@@ -1 +1 @@\n-old\n+middle\n"),
		fileChangeMessage("/work/a.go", `{"type":"update"}`, "@@ -1 +1 @@\n-middle\n+final\n"),
	}
	result := SummarizeChanges("/work", messages)
	if len(result.Files) != 1 || result.Added != 1 || result.Deleted != 1 || !result.Exact {
		t.Fatal("relative and absolute paths counted separately", result)
	}
	messages[1].Tool.Changes[0].Truncated = true
	if got := SummarizeChanges("/work", messages); got.Exact {
		t.Fatal("per-file truncation presented as complete")
	}
}
