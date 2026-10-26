package editor

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Only the isolated test executable kills itself; the parent verifies both disk
// content and the independently readable recovery envelope after termination.
func TestAtomicWriteCrashHelper(t *testing.T) {
	if os.Getenv("FUYU_CRASH_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	root := os.Getenv("FUYU_CRASH_ROOT")
	kind, stage := os.Getenv("FUYU_CRASH_KIND"), os.Getenv("FUYU_CRASH_STAGE")
	base, err := Read(filepath.Join(root, "document.txt"))
	if err != nil {
		t.Fatal(err)
	}
	crash := func() {
		if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	replace := func(from, to string) error {
		if stage == "before-replace" {
			crash()
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		crash()
		return nil
	}
	switch kind {
	case "save":
		ops := diskSaveOperations()
		ops.rename = replace
		_, err = saveWith(context.Background(), base, "latest draft\n", false, ops)
	case "checkpoint":
		ops := diskRecoveryOperations()
		ops.rename = replace
		err = writeRecoveryWith(filepath.Join(root, "recovery"), Recovery{
			ID: RecoveryID(root, base.Path), Workspace: root, Path: base.Path,
			Base: base, Text: "latest draft\n",
		}, ops)
	case "cleanup":
		if stage == "before-remove" {
			crash()
		}
		if err := RemoveRecovery(filepath.Join(root, "recovery"), RecoveryID(root, base.Path)); err != nil {
			t.Fatal(err)
		}
		crash()
	default:
		t.Fatalf("unknown operation %q", kind)
	}
	t.Fatalf("helper did not reach termination boundary: %v", err)
}

func TestAtomicWriteSurvivesProcessCrash(t *testing.T) {
	for _, kind := range []string{"save", "checkpoint", "cleanup"} {
		stages := []string{"before-replace", "after-replace"}
		if kind == "cleanup" {
			stages = []string{"before-remove", "after-remove"}
		}
		for _, stage := range stages {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "document.txt")
				if err := os.WriteFile(path, []byte("original\n"), 0640); err != nil {
					t.Fatal(err)
				}
				base, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				previousDraft := "previous draft\n"
				if kind != "checkpoint" {
					previousDraft = "latest draft\n"
				}
				recoveryDir := filepath.Join(root, "recovery")
				id := RecoveryID(root, path)
				if err := WriteRecovery(recoveryDir, Recovery{
					ID: id, Workspace: root, Path: path, Base: base, Text: previousDraft,
				}); err != nil {
					t.Fatal(err)
				}
				if kind == "cleanup" {
					if _, err := Save(context.Background(), base, previousDraft, false); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAtomicWriteCrashHelper$")
				cmd.Env = append(os.Environ(), "FUYU_CRASH_HELPER=1", "FUYU_CRASH_ROOT="+root,
					"FUYU_CRASH_KIND="+kind, "FUYU_CRASH_STAGE="+stage)
				output, err := cmd.CombinedOutput()
				var exit *exec.ExitError
				if ctx.Err() != nil || !errors.As(err, &exit) {
					t.Fatalf("expected helper SIGKILL: %v, context %v, %s", err, ctx.Err(), output)
				}
				status, ok := exit.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatalf("unexpected helper exit: %v, %s", err, output)
				}
				raw, err := os.ReadFile(path)
				wantDisk := "original\n"
				if kind == "cleanup" || kind == "save" && stage == "after-replace" {
					wantDisk = "latest draft\n"
				}
				if err != nil || string(raw) != wantDisk {
					t.Fatalf("disk = %q, want %q: %v", raw, wantDisk, err)
				}
				records, err := Recoveries(recoveryDir)
				if kind == "cleanup" && stage == "after-remove" {
					if err != nil || len(records) != 0 {
						t.Fatalf("completed cleanup resurrected a recovery: %v, %#v", err, records)
					}
					return
				}
				wantDraft := previousDraft
				if kind == "checkpoint" && stage == "after-replace" {
					wantDraft = "latest draft\n"
				}
				if err != nil || len(records) != 1 {
					t.Fatalf("recovery envelope missing/corrupt: %v, %#v", err, records)
				}
				r := records[0]
				if !r.Valid() || r.ID != id || r.Text != wantDraft || r.DiskText != "original\n" {
					t.Fatalf("unexpected recovery after crash: %#v", r)
				}
			})
		}
	}
}

func TestCheckpointFailurePreservesLastCompleteEnvelope(t *testing.T) {
	for _, stage := range []string{"write-full", "short-write", "sync-file", "rename", "sync-directory"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "document.txt")
			if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
				t.Fatal(err)
			}
			base, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			r := Recovery{ID: RecoveryID(root, path), Workspace: root, Path: path, Base: base, Text: "previous\n"}
			dir := filepath.Join(root, "recovery")
			if err := WriteRecovery(dir, r); err != nil {
				t.Fatal(err)
			}
			ops := diskRecoveryOperations()
			switch stage {
			case "write-full", "short-write":
				ops.write = func(f *os.File, raw []byte) (int, error) {
					n, err := f.Write(raw[:3])
					if err != nil {
						return n, err
					}
					if stage == "write-full" {
						return n, syscall.ENOSPC
					}
					return n, nil
				}
			case "sync-file":
				ops.sync = func(*os.File) error { return syscall.EIO }
			case "rename":
				ops.rename = func(string, string) error { return syscall.EACCES }
			case "sync-directory":
				ops.syncDir = func(string) error { return syscall.EIO }
			}
			r.Text = "latest\n"
			err = writeRecoveryWith(dir, r, ops)
			if err == nil || stage == "short-write" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("checkpoint failure not reported: %v", err)
			}
			records, err := Recoveries(dir)
			if err != nil || len(records) != 1 || !records[0].Valid() {
				t.Fatalf("last complete envelope lost: %v, %#v", err, records)
			}
			want := "previous\n"
			if stage == "sync-directory" {
				want = "latest\n"
			}
			if records[0].Text != want || records[0].DiskText != base.Text {
				t.Fatal("checkpoint failure changed baseline or lost complete draft", records[0])
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, ".checkpoint-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatal("failed checkpoint left temporary files", leftovers, err)
			}
		})
	}
}
