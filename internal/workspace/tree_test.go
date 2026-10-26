package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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
