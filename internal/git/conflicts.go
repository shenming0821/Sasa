package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type ConflictBlock struct {
	Start, End              int
	Base, Current, Incoming string
}

// ConflictBlocks retains byte ranges so accepting a block leaves other text
// byte-for-byte intact, including CRLF and missing final newlines.
func ConflictBlocks(text string) []ConflictBlock {
	var blocks []ConflictBlock
	lines := strings.SplitAfter(text, "\n")
	offset, start, body, base, separator := 0, -1, -1, -1, -1
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "<<<<<<< "):
			start, body, base, separator = offset, offset+len(line), -1, -1
		case start >= 0 && strings.HasPrefix(line, "||||||| "):
			base = offset
		case start >= 0 && strings.TrimRight(line, "\r\n") == "=======":
			separator = offset
		case start >= 0 && separator >= 0 && strings.HasPrefix(line, ">>>>>>> "):
			middleEnd := strings.IndexByte(text[separator:], '\n')
			if middleEnd < 0 {
				continue
			}
			currentEnd := separator
			baseText := ""
			if base >= 0 {
				currentEnd = base
				if end := strings.IndexByte(text[base:], '\n'); end >= 0 {
					baseText = text[base+end+1 : separator]
				}
			}
			blocks = append(blocks, ConflictBlock{
				Start: start, End: offset + len(line),
				Current: text[body:currentEnd], Base: baseText,
				Incoming: text[separator+middleEnd+1 : offset],
			})
			start = -1
		}
		offset += len(line)
	}
	return blocks
}

func MarkResolved(ctx context.Context, root, path string) error {
	if err := validPaths([]string{path}); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(root, path))
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > OutputLimit {
			return errors.New("请在外部处理此类型的冲突文件")
		}
		data, readErr := os.ReadFile(filepath.Join(root, path))
		if readErr != nil {
			return readErr
		}
		if HasConflictMarkers(string(data)) {
			return errors.New("文件仍包含冲突标记，请先解决并保存")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return Stage(ctx, root, []string{path})
}

// Incomplete markers also block resolution after manual editing.
func HasConflictMarkers(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, ">>>>>>> ") ||
			strings.HasPrefix(line, "||||||| ") || line == "=======" {
			return true
		}
	}
	return false
}
