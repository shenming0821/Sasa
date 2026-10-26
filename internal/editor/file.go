// Package editor owns disk versions and recovery copies, independently of UI focus.
package editor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"
)

const MaxBytes = 16 << 20
const EditBytes = 4 << 20

var ErrConflict = errors.New("文件已在外部更改")
var recoveryMu sync.Mutex

type Stamp struct {
	Hash     string
	Mode     os.FileMode
	Size     int64
	Identity string
	Parent   string
}
type File struct {
	Path, Text, Reason string
	BOM, CRLF          bool
	Stamp              Stamp
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Read(path string) (File, error) {
	f := File{Path: path}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return f, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return f, err
	}
	linked := info.Mode()&os.ModeSymlink != 0
	if linked {
		info, err = os.Stat(path)
		if err != nil {
			return f, err
		}
	}
	if !info.Mode().IsRegular() {
		return f, fmt.Errorf("不是普通文件：%s", path)
	}
	if info.Size() > MaxBytes {
		return f, fmt.Errorf("文件超过 16 MiB，请使用外部编辑器打开")
	}
	openPath := path
	if linked {
		openPath, err = filepath.EvalSymlinks(path)
		if err != nil {
			return f, err
		}
	}
	fd, err := os.OpenFile(openPath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return f, err
	}
	defer fd.Close()
	now, err := fd.Stat()
	if err != nil {
		return f, err
	}
	if !os.SameFile(info, now) {
		return f, ErrConflict
	}
	raw, err := io.ReadAll(io.LimitReader(fd, MaxBytes+1))
	if err != nil {
		return f, err
	}
	if len(raw) > MaxBytes {
		return f, fmt.Errorf("文件读取期间增长，超过 16 MiB")
	}
	f.Stamp = stamp(raw, now)
	f.Stamp.Parent = parent
	if currentParent, err := filepath.EvalSymlinks(filepath.Dir(path)); err != nil || currentParent != parent {
		return f, ErrConflict
	}
	if bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		f.Reason = "二进制或不支持的编码，请在外部打开"
		return f, nil
	}
	f.BOM = bytes.HasPrefix(raw, []byte{239, 187, 191})
	if f.BOM {
		raw = raw[3:]
	}
	f.CRLF = bytes.Contains(raw, []byte("\r\n"))
	text := string(raw)
	if f.CRLF && strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		f.Reason = "混合换行，当前只读"
	}
	f.Text = strings.ReplaceAll(text, "\r\n", "\n")
	if linked {
		f.Reason = "符号链接，当前只读"
	}
	if st, ok := now.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
		f.Reason = "硬链接，当前只读"
	}
	if len(raw) > EditBytes || strings.Count(f.Text, "\n") >= 100000 {
		f.Reason = "大文件只读模式"
	}
	for _, line := range strings.Split(f.Text, "\n") {
		if len(line) > 256<<10 {
			f.Text = ""
			f.Reason = "单行超过 256 KiB，请在外部打开"
			break
		}
	}
	return f, nil
}
func stamp(raw []byte, info os.FileInfo) Stamp {
	s := Stamp{Hash: digest(raw), Mode: info.Mode(), Size: int64(len(raw))}
	if v, ok := info.Sys().(*syscall.Stat_t); ok {
		s.Identity = fmt.Sprintf("%d:%d", v.Dev, v.Ino)
	}
	return s
}
func (f File) Bytes(text string) []byte {
	if f.CRLF {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if f.BOM {
		text = "\xef\xbb\xbf" + text
	}
	return []byte(text)
}
func (f File) Changed(other File) bool {
	return f.Stamp.Hash != other.Stamp.Hash || f.Stamp.Identity != other.Stamp.Identity || f.Stamp.Mode != other.Stamp.Mode || f.Stamp.Parent != other.Stamp.Parent
}

// Save writes one immutable version. Callers serialize saves of each document.
func Save(ctx context.Context, base File, text string, recreate bool) (File, error) {
	return saveWith(ctx, base, text, recreate, diskSaveOperations())
}

// Each IO boundary is explicit so failures before and after replacement can be verified.
type saveOperations struct {
	read    func(string) (File, error)
	write   func(*os.File, []byte) (int, error)
	sync    func(*os.File) error
	rename  func(string, string) error
	syncDir func(string) error
}

func diskSaveOperations() saveOperations {
	return saveOperations{
		read: Read, write: func(f *os.File, raw []byte) (int, error) { return f.Write(raw) },
		sync: (*os.File).Sync, rename: os.Rename, syncDir: syncDirectory,
	}
}

func saveWith(ctx context.Context, base File, text string, recreate bool, ops saveOperations) (File, error) {
	current, err := ops.read(base.Path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !(missing && recreate) {
		return base, err
	}
	if !missing && (base.Changed(current) || current.Reason != "") {
		return base, ErrConflict
	}
	if missing {
		parent, err := filepath.EvalSymlinks(filepath.Dir(base.Path))
		if err != nil || parent != base.Stamp.Parent {
			return base, ErrConflict
		}
	}
	raw := base.Bytes(text)
	if len(raw) > EditBytes {
		return base, fmt.Errorf("编辑内容超过 4 MiB")
	}
	tmp, err := os.CreateTemp(filepath.Dir(base.Path), ".fuyu-save-*")
	if err != nil {
		return base, err
	}
	name := tmp.Name()
	tmp.Close()
	defer os.Remove(name)
	// cp -p preserves platform ACLs and extended attributes as well as mode.
	if !missing {
		if out, e := exec.CommandContext(ctx, "/bin/cp", "-p", base.Path, name).CombinedOutput(); e != nil {
			return base, fmt.Errorf("保留文件属性：%w %s", e, out)
		}
	}
	fd, err := os.OpenFile(name, os.O_WRONLY|os.O_TRUNC, base.Stamp.Mode.Perm())
	if err != nil {
		return base, err
	}
	if missing {
		err = fd.Chmod(base.Stamp.Mode.Perm())
	}
	if err == nil {
		var n int
		n, err = ops.write(fd, raw)
		if err == nil && n != len(raw) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = ops.sync(fd)
	}
	closeErr := fd.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return base, err
	}
	select {
	case <-ctx.Done():
		return base, ctx.Err()
	default:
	}
	check, e := ops.read(base.Path)
	if (!missing && (e != nil || current.Changed(check))) || (missing && !errors.Is(e, os.ErrNotExist)) {
		return base, ErrConflict
	}
	if err = ops.rename(name, base.Path); err != nil {
		return base, err
	}
	result, e := ops.read(base.Path)
	if e != nil {
		return base, fmt.Errorf("文件已替换，读取核验失败：%w", e)
	}
	if result.Stamp.Hash != digest(raw) {
		return base, fmt.Errorf("文件已替换，但随后再次发生变化：%w", ErrConflict)
	}
	e = ops.syncDir(filepath.Dir(base.Path))
	if e != nil {
		return result, fmt.Errorf("文件已替换，目录同步失败：%w", e)
	}
	return result, nil
}

type Recovery struct {
	ID, Workspace, Path, Text, Hash string
	DiskText                        string
	Base                            File
	Version                         uint64
}

func (r Recovery) Valid() bool {
	rel, err := filepath.Rel(r.Workspace, r.Path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		r.ID == RecoveryID(r.Workspace, r.Path) && r.Hash == digest([]byte(r.Text)) &&
		(r.Base.Stamp.Hash == "" || r.Base.Stamp.Hash == digest(r.Base.Bytes(r.DiskText))) &&
		filepath.IsAbs(r.Path) && filepath.IsAbs(r.Workspace)
}
func RecoveryID(workspace, path string) string { return digest([]byte(workspace + "\x00" + path)) }
func WriteRecovery(dir string, r Recovery) error {
	return writeRecoveryWith(dir, r, diskRecoveryOperations())
}

type recoveryOperations struct {
	write   func(*os.File, []byte) (int, error)
	sync    func(*os.File) error
	rename  func(string, string) error
	syncDir func(string) error
}

func diskRecoveryOperations() recoveryOperations {
	return recoveryOperations{
		write: func(f *os.File, raw []byte) (int, error) { return f.Write(raw) },
		sync:  (*os.File).Sync, rename: os.Rename, syncDir: syncDirectory,
	}
}

func writeRecoveryWith(dir string, r Recovery, ops recoveryOperations) error {
	recoveryMu.Lock()
	defer recoveryMu.Unlock()
	if r.ID == "" || filepath.Base(r.ID) != r.ID {
		return errors.New("无效恢复 ID")
	}
	r.Hash = digest([]byte(r.Text))
	r.DiskText = r.Base.Text
	r.Base.Text = ""
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	total := int64(len(raw))
	for _, entry := range entries {
		if entry.Name() == r.ID+".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
	}
	if total > 256<<20 {
		return errors.New("恢复副本已达 256 MiB，请先保存文件或处理已有恢复副本")
	}
	f, err := os.CreateTemp(dir, ".checkpoint-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		var n int
		n, err = ops.write(f, raw)
		if err == nil && n != len(raw) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = ops.sync(f)
	}
	e := f.Close()
	if err == nil {
		err = e
	}
	if err == nil {
		err = ops.rename(name, filepath.Join(dir, r.ID+".json"))
	}
	if err == nil {
		err = ops.syncDir(dir)
	}
	return err
}
func syncDirectory(path string) error {
	fd, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fd.Close()
	return fd.Sync()
}
func Recoveries(dir string) ([]Recovery, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []Recovery
	var failures []error
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, e := entry.Info()
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if !info.Mode().IsRegular() || info.Size() > 2*MaxBytes {
			failures = append(failures, fmt.Errorf("无效恢复副本：%s", entry.Name()))
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		var r Recovery
		if e = json.Unmarshal(raw, &r); e != nil || !r.Valid() || entry.Name() != r.ID+".json" {
			failures = append(failures, fmt.Errorf("恢复副本损坏，已保留：%s", entry.Name()))
			continue
		}
		result = append(result, r)
	}
	return result, errors.Join(failures...)
}
func RemoveRecovery(dir, id string) error {
	recoveryMu.Lock()
	defer recoveryMu.Unlock()
	if id == "" || filepath.Base(id) != id {
		return errors.New("无效恢复 ID")
	}
	err := os.Remove(filepath.Join(dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		err = syncDirectory(dir)
	}
	return err
}
