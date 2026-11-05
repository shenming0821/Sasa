package workspace

import (
	"context"
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
	for range 60 {
		many.WriteString("needle line\n")
	}
	write("many.txt", many.String())

	result, err := SearchContent(context.Background(), root, root, "needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 52 || !result.Truncated {
		t.Fatal("expected 50 capped lines plus 2 note matches and truncation", len(result.Matches), result.Truncated)
	}
	byPath := map[string][]ContentMatch{}
	for _, m := range result.Matches {
		byPath[m.Path] = append(byPath[m.Path], m)
	}
	if len(byPath[filepath.Join(root, "many.txt")]) != 50 || len(byPath[filepath.Join(root, "sub", "note.md")]) != 2 {
		t.Fatal("matches grouped by path wrong", byPath)
	}
	if len(byPath[filepath.Join(root, "binary.bin")]) != 0 || len(byPath[filepath.Join(root, "node_modules", "pkg", "x.go")]) != 0 {
		t.Fatal("binary or skipped directory leaked matches")
	}
	second := byPath[filepath.Join(root, "sub", "note.md")][1]
	if second.Line != 2 || second.Col != 1 || !strings.Contains(second.Text, "needle 你好 second") {
		t.Fatal("match location or preview wrong", second)
	}

	cjk, err := SearchContent(context.Background(), root, root, "fuyu")
	if err != nil || len(cjk.Matches) != 1 {
		t.Fatal(cjk, err)
	}
	if hit := cjk.Matches[0]; hit.Line != 2 || hit.Col != 7 {
		t.Fatal("CJK column misaligned", hit)
	}

	scoped, err := SearchContent(context.Background(), root, filepath.Join(root, "sub"), "needle")
	if err != nil || len(scoped.Matches) != 2 || scoped.Truncated {
		t.Fatal("scoped search wrong", scoped, err)
	}
	if _, err := SearchContent(context.Background(), root, filepath.Dir(root), "needle"); err == nil {
		t.Fatal("search escaped workspace root")
	}
	if names, err := Search(context.Background(), filepath.Join(root, "sub"), "note"); err != nil || len(names.Entries) != 1 {
		t.Fatal("scoped filename search wrong", names, err)
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
