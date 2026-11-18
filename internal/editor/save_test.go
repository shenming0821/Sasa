package editor

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestSaveFailureStagesPreserveOriginalOrReportReplacement(t *testing.T) {
	for _, stage := range []string{"write-full", "short-write", "sync-file", "rename", "readback", "sync-directory"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "document.txt")
			if err := os.WriteFile(path, []byte("original\n"), 0640); err != nil {
				t.Fatal(err)
			}
			base, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			ops := diskSaveOperations()
			replaced := false
			ops.rename = func(from, to string) error {
				if stage == "rename" {
					return syscall.EACCES
				}
				err := os.Rename(from, to)
				replaced = err == nil
				return err
			}
			ops.read = func(path string) (File, error) {
				if stage == "readback" && replaced {
					return File{}, syscall.EIO
				}
				return Read(path)
			}
			switch stage {
			case "write-full", "short-write":
				ops.write = func(f *os.File, b []byte) (int, error) {
					n, err := f.Write(b[:3])
					if err != nil {
						return n, err
					}
					if stage == "write-full" {
						return n, syscall.ENOSPC
					}
					return n, nil
				}
			case "sync-file":
				ops.sync = func(*os.File) error { return syscall.ENOSPC }
			case "sync-directory":
				ops.syncDir = func(string) error { return syscall.EIO }
			}
			_, err = saveWith(context.Background(), base, "local draft\n", false, ops)
			if err == nil {
				t.Fatal("injected IO failure was ignored")
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := "original\n"
			if replaced {
				want = "local draft\n"
				if !strings.Contains(err.Error(), "文件已替换") {
					t.Fatal("post-replacement failure did not describe actual disk state", err)
				}
			}
			if string(raw) != want {
				t.Fatal("failure left unexpected disk content", string(raw), err)
			}
			if stage == "short-write" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal("partial write was not rejected before replacement", err)
			}
			temporary, err := filepath.Glob(filepath.Join(root, ".sasa-save-*"))
			if err != nil || len(temporary) != 0 {
				t.Fatal("save left unfinished temporary files", temporary, err)
			}
			if base.Text != "original\n" {
				t.Fatal("failed save changed immutable caller baseline")
			}
		})
	}
}

func TestSavePreservesMacExtendedAttributes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS extended attributes")
	}
	path := filepath.Join(t.TempDir(), "document.txt")
	if err := os.WriteFile(path, []byte("original\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/xattr", "-w", "com.sasa.p4-test", "metadata", path).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	base, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(context.Background(), base, "changed\n", false); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/usr/bin/xattr", "-p", "com.sasa.p4-test", path).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "metadata" {
		t.Fatal("atomic save stripped extended attributes", err, string(out))
	}
}
