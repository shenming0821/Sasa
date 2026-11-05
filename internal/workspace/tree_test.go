package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLazyTreeSearchAndRootBoundary(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "sub"), 0700)
	os.Mkdir(filepath.Join(root, "node_modules"), 0700)
	os.WriteFile(filepath.Join(root, "sub", "中文.go"), []byte("package x"), 0600)
	os.WriteFile(filepath.Join(root, "node_modules", "secret.go"), nil, 0600)
	os.Symlink(root, filepath.Join(root, "loop"))
	entries, e := ReadDir(context.Background(), root, root)
	if e != nil || len(entries) != 3 {
		t.Fatal(entries, e)
	}
	result, e := Search(context.Background(), root, ".go")
	if e != nil || len(result.Entries) != 1 || result.Entries[0].Name != "中文.go" {
		t.Fatal(result, e)
	}
	if Contains(root, root+"-outside/file") {
		t.Fatal("prefix collision accepted")
	}
	if _, e = ReadDir(context.Background(), root, filepath.Join(root, "loop")); e == nil {
		t.Fatal("symlink traversed")
	}
}

func TestContentSearchScopeSkipsAndTruncates(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n// 你好 Fuyu\nfunc main() {}\n")
	write("sub/note.md", "Needle line\nneedle 你好 second\n")
	write("node_modules/pkg/x.go", "needle hidden\n")
	write("binary.bin", "needle\x00inside\n")
	var many strings.Builder
	for range 120 {
		many.WriteString("needle line\n")
	}
	write("many.txt", many.String())

	result, err := SearchContent(context.Background(), root, ContentQuery{Text: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 102 || !result.Truncated {
		t.Fatal("expected 100 capped lines plus 2 note matches and truncation", len(result.Matches), result.Truncated)
	}
	byPath := map[string][]ContentMatch{}
	for _, m := range result.Matches {
		byPath[m.Path] = append(byPath[m.Path], m)
	}
	if len(byPath[filepath.Join(root, "many.txt")]) != 100 || len(byPath[filepath.Join(root, "sub", "note.md")]) != 2 {
		t.Fatal("matches grouped by path wrong", byPath)
	}
	if len(byPath[filepath.Join(root, "binary.bin")]) != 0 || len(byPath[filepath.Join(root, "node_modules", "pkg", "x.go")]) != 0 {
		t.Fatal("binary or skipped directory leaked matches")
	}
	second := byPath[filepath.Join(root, "sub", "note.md")][1]
	if second.Line != 2 || second.Col != 1 || !strings.Contains(second.Text, "needle 你好 second") || !slices.Equal(second.Ranges, []MatchRange{{0, 6}}) {
		t.Fatal("match location or preview wrong", second)
	}

	cjk, err := SearchContent(context.Background(), root, ContentQuery{Text: "fuyu"})
	if err != nil || len(cjk.Matches) != 1 {
		t.Fatal(cjk, err)
	}
	if hit := cjk.Matches[0]; hit.Line != 2 || hit.Col != 7 || !slices.Equal(hit.Ranges, []MatchRange{{6, 10}}) {
		t.Fatal("CJK column misaligned", hit)
	}

	scoped, err := SearchContent(context.Background(), root, ContentQuery{Text: "needle", Include: "./sub"})
	if err != nil || len(scoped.Matches) != 2 || scoped.Truncated {
		t.Fatal("scoped search wrong", scoped, err)
	}
	if escaped, err := SearchContent(context.Background(), root, ContentQuery{Text: "needle", Include: "./../"}); err != nil || len(escaped.Matches) != 0 {
		t.Fatal("search escaped workspace root", escaped, err)
	}
	if names, err := Search(context.Background(), filepath.Join(root, "sub"), "note"); err != nil || len(names.Entries) != 1 {
		t.Fatal("scoped filename search wrong", names, err)
	}
}

func TestContentSearchOptionsAndGlobs(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/ui/view.go", "Mode mode modeline\n\tx := 0o111 // 111\n")
	write("internal/ui/view_test.go", "mode\n")
	write("internal/testdata/fixture.go", "mode\n")
	write("cmd/main.go", "mode\n")
	write("README.md", "mode\n")
	write("node_modules/pkg/index.js", "mode\n")
	search := func(q ContentQuery) []string {
		t.Helper()
		result, err := SearchContent(context.Background(), root, q)
		if err != nil {
			t.Fatal(q, err)
		}
		var hits []string
		for _, m := range result.Matches {
			hits = append(hits, fmt.Sprintf("%s:%d:%d:%d", filepath.ToSlash(strings.TrimPrefix(m.Path, root+string(filepath.Separator))), m.Line, m.Col, m.Count()))
		}
		return hits
	}
	want := func(name string, got []string, expected ...string) {
		t.Helper()
		if !slices.Equal(got, expected) {
			t.Fatalf("%s: got %q want %q", name, got, expected)
		}
	}
	want("case insensitive", search(ContentQuery{Text: "mode", Include: "./internal/ui"}), "internal/ui/view.go:1:1:3", "internal/ui/view_test.go:1:1:1")
	want("match case", search(ContentQuery{Text: "Mode", MatchCase: true, Include: "./internal/ui"}), "internal/ui/view.go:1:1:1")
	want("whole word", search(ContentQuery{Text: "mode", WholeWord: true, Include: "view.go"}), "internal/ui/view.go:1:1:2")
	want("regex", search(ContentQuery{Text: `0o?1{3}`, Regex: true, Include: "*.go", Exclude: "*_test.go"}), "internal/ui/view.go:2:7:1")
	want("unanchored folder glob", search(ContentQuery{Text: "mode", Include: "cmd"}), "cmd/main.go:1:1:1")
	want("braces and double star", search(ContentQuery{Text: "mode", Include: "./internal/**/*.go", Exclude: "{testdata,ui}"}))
	want("exclude folder", search(ContentQuery{Text: "mode", Include: "./internal", Exclude: "**/testdata, *_test.go"}), "internal/ui/view.go:1:1:3")
	want("default skips dependencies", search(ContentQuery{Text: "mode", Include: "*.js"}))
	want("search ignored", search(ContentQuery{Text: "mode", Include: "*.js", SearchIgnored: true}), "node_modules/pkg/index.js:1:1:1")
	want("anchored file", search(ContentQuery{Text: "mode", Include: "./README.md"}), "README.md:1:1:1")
	if _, err := SearchContent(context.Background(), root, ContentQuery{Text: "(", Regex: true}); err == nil || !strings.Contains(err.Error(), "正则表达式无效") {
		t.Fatal("invalid regex accepted", err)
	}
	if _, err := SearchContent(context.Background(), root, ContentQuery{Text: "x", Include: "{a,b"}); err == nil || !strings.Contains(err.Error(), "文件模式无效") {
		t.Fatal("invalid glob accepted", err)
	}
}

func TestMatchPreviewRangesOnLongLines(t *testing.T) {
	line := "    " + strings.Repeat("中", 300) + "needle" + strings.Repeat("x", 300)
	start := strings.Index(line, "needle")
	text, ranges := matchPreview(line, [][]int{{start, start + len("needle")}})
	runes := []rune(text)
	if len(ranges) != 1 || string(runes[ranges[0].Start:ranges[0].End]) != "needle" || !strings.HasPrefix(text, "…") || !strings.HasSuffix(text, "…") {
		t.Fatal("long line preview lost the match", ranges, text)
	}
	line = "\t// " + strings.Repeat("word ", 12) + "needle tail"
	start = strings.Index(line, "needle")
	text, ranges = matchPreview(line, [][]int{{start, start + len("needle")}})
	if len(ranges) != 1 || ranges[0].Start != 1+contentSearchPreviewLeadIn || !strings.HasPrefix(text, "…") || !strings.HasSuffix(text, "needle tail") {
		t.Fatal("narrow panel preview should start shortly before the match", ranges, text)
	}
	text, ranges = matchPreview("  short needle", [][]int{{8, 14}})
	if text != "short needle" || ranges[0] != (MatchRange{6, 12}) {
		t.Fatal("short line preview changed", ranges, text)
	}
}

func TestNaturalOrderAndSymlinkAncestor(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"file10.go", "file02.go", "file2.go", "File1.go"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ReadDir(context.Background(), root, root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"File1.go", "file2.go", "file02.go", "file10.go"}) {
		t.Fatal("not natural order", names)
	}
	outside := t.TempDir()
	os.Mkdir(filepath.Join(outside, "sub"), 0700)
	os.Symlink(outside, filepath.Join(root, "link"))
	if _, err := ReadDir(context.Background(), root, filepath.Join(root, "link", "sub")); err == nil {
		t.Fatal("symlink ancestor escaped workspace")
	}
}
