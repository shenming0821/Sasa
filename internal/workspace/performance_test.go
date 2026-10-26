package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestP4TenThousandPathsPerformance(t *testing.T) {
	if os.Getenv("FUYU_P4_PERF") != "1" {
		t.Skip("opt-in P4 performance sample")
	}
	root := t.TempDir()
	for i := range 100 {
		dir := filepath.Join(root, fmt.Sprintf("dir%03d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for j := range 99 {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%03d.txt", j)), []byte("fixture\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	rows, err := ReadDir(ctx, root, root)
	treeTime := time.Since(start)
	if err != nil || len(rows) != 100 {
		t.Fatal("lazy root load", len(rows), err)
	}
	start = time.Now()
	result, err := Search(ctx, root, "dir099/file098")
	searchTime := time.Since(start)
	if err != nil || len(result.Entries) != 1 || result.Truncated {
		t.Fatal("bounded complete path search", result, err)
	}
	t.Logf("warm filesystem; freshly created ordinary directory; 10,000 paths: root listing=%s (%d direct rows), exact-path search=%s", treeTime, len(rows), searchTime)
}
