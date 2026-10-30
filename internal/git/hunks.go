package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Hunk struct {
	Header, Patch                          string
	OldStart, OldCount, NewStart, NewCount int
	PatchLine                              int
}

type Review struct {
	Entry             Entry
	Staged            bool
	Patch, Header     string
	Hunks             []Hunk
	Token             string
	Added, Deleted    int
	Binary, Blockable bool
	BeforeText        string
	BeforeKnown       bool
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func ParsePatch(patch string) Review {
	review := Review{Patch: patch, Binary: strings.Contains(patch, "GIT binary patch") || strings.Contains(patch, "Binary files ")}
	lines := strings.SplitAfter(patch, "\n")
	headerEnd := 0
	for line, text := range lines {
		match := hunkHeader.FindStringSubmatch(text)
		if match != nil {
			count := func(s string) int {
				if s == "" {
					return 1
				}
				n, _ := strconv.Atoi(s)
				return n
			}
			if len(review.Hunks) == 0 {
				headerEnd = line
			}
			review.Hunks = append(review.Hunks, Hunk{
				Header: strings.TrimSuffix(text, "\n"), PatchLine: line,
				OldStart: count(match[1]), OldCount: count(match[2]),
				NewStart: count(match[3]), NewCount: count(match[4]),
			})
		}
		if len(review.Hunks) > 0 {
			if strings.HasPrefix(text, "+") {
				review.Added++
			} else if strings.HasPrefix(text, "-") {
				review.Deleted++
			}
		}
	}
	if len(review.Hunks) > 0 {
		review.Header = strings.Join(lines[:headerEnd], "")
		for i := range review.Hunks {
			end := len(lines)
			if i+1 < len(review.Hunks) {
				end = review.Hunks[i+1].PatchLine
			}
			review.Hunks[i].Patch = strings.Join(lines[review.Hunks[i].PatchLine:end], "")
		}
	}
	return review
}

func reviewToken(ctx context.Context, root string, entry Entry) (string, bool, error) {
	if err := validPaths([]string{entry.Path}); err != nil {
		return "", false, err
	}
	args := []string{"ls-files", "--stage", "-z", "--", entry.Path}
	if entry.Original != "" {
		args = append(args, entry.Original)
	}
	index, err := run(ctx, root, args...)
	if err != nil {
		return "", false, err
	}
	head, headErr := run(ctx, root, "rev-parse", "--verify", "HEAD")
	if headErr != nil && ctx.Err() != nil {
		return "", false, ctx.Err()
	}
	index = append(index, append([]byte("\x00HEAD:"), head...)...)
	path := filepath.Join(root, entry.Path)
	parent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
	base, rootErr := filepath.EvalSymlinks(root)
	if parentErr == nil && rootErr == nil {
		rel, relErr := filepath.Rel(base, parent)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false, errors.New("文件父目录已移出工作区")
		}
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return fingerprint(append(index, []byte("\x00missing")...)), true, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() || info.Size() > OutputLimit {
		return fingerprint(index), false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	return fingerprint(append(index, []byte(fmt.Sprintf("\x00%d:%s", info.Mode(), fingerprint(data)))...)), true, nil
}

func ReadReview(ctx context.Context, root string, entry Entry, staged bool) (Review, error) {
	before, allowed, err := reviewToken(ctx, root, entry)
	if err != nil {
		return Review{}, err
	}
	patch, err := Diff(ctx, root, entry, staged)
	if err != nil {
		return Review{}, err
	}
	review := ParsePatch(patch)
	reference := ":"
	if staged {
		reference = "HEAD:"
	}
	ReadReviewBase(ctx, root, reference, entry, &review)
	after, _, err := reviewToken(ctx, root, entry)
	if err != nil {
		return Review{}, err
	}
	if before != after {
		return Review{}, errors.New("读取期间文件或暂存区发生变化，请刷新")
	}
	review.Entry, review.Staged, review.Token = entry, staged, after
	review.Blockable = allowed && !entry.Conflict && !entry.Submodule && !review.Binary && entry.Original == ""
	return review, nil
}

func ReadReviewBase(ctx context.Context, root, reference string, entry Entry, review *Review) {
	if review.Binary || len(review.Hunks) == 0 {
		return
	}
	if review.Hunks[0].OldStart == 0 && review.Hunks[0].OldCount == 0 {
		review.BeforeKnown = true
		return
	}
	path := entry.Path
	if entry.Original != "" {
		path = entry.Original
	}
	if validPaths([]string{path}) != nil {
		return
	}
	text, err := run(ctx, root, "show", reference+path)
	if err == nil && !strings.ContainsRune(string(text), '\x00') {
		review.BeforeText, review.BeforeKnown = string(text), true
	}
}

func ApplyHunk(ctx context.Context, root string, review Review, index int) error {
	if !review.Blockable || index < 0 || index >= len(review.Hunks) {
		return errors.New("当前差异不支持此修改块操作")
	}
	token, _, err := reviewToken(ctx, root, review.Entry)
	if err != nil {
		return err
	}
	if token != review.Token {
		return errors.New("文件或暂存区已变化，请刷新差异后重试")
	}
	patch := review.Header + review.Hunks[index].Patch
	args := []string{"apply", "--cached", "--whitespace=nowarn"}
	if review.Staged {
		args = append(args, "--reverse")
	}
	if _, err = commandInput(ctx, root, []byte(patch), append(args, "--check", "-")...); err != nil {
		return fmt.Errorf("修改块已不适用于当前暂存区：%w", err)
	}
	_, err = commandInput(ctx, root, []byte(patch), append(args, "-")...)
	return err
}
