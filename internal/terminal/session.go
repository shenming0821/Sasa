package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"unsafe"
)

type Session struct {
	root     string
	handle   unsafe.Pointer
	token    uintptr
	closed   atomic.Bool
	exited   atomic.Bool
	callback sync.RWMutex
	shortcut func(string)
}

func Start(cwd string) (*Session, error) {
	if !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("终端工作目录必须是绝对路径")
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return nil, fmt.Errorf("读取终端工作目录：%w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("终端工作目录不是文件夹：%s", cwd)
	}
	return &Session{root: filepath.Clean(cwd)}, nil
}

func (s *Session) Exited() bool { return s.closed.Load() || s.exited.Load() }

func (s *Session) SetShortcut(callback func(string)) {
	s.callback.Lock()
	s.shortcut = callback
	s.callback.Unlock()
}

func (s *Session) notify(key string) {
	s.callback.RLock()
	callback := s.shortcut
	s.callback.RUnlock()
	if callback != nil && !s.closed.Load() {
		go callback(key)
	}
}

func libraryPaths(executable string) (string, string) {
	contents := filepath.Dir(filepath.Dir(executable))
	if filepath.Base(contents) == "Contents" {
		return filepath.Join(contents, "Frameworks", "libghostty.dylib"), filepath.Join(contents, "Resources", "ghostty")
	}
	return filepath.Join(filepath.Dir(executable), "libghostty.dylib"), filepath.Join(filepath.Dir(executable), "ghostty")
}
