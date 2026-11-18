// Portions adapted from Mayoi, Copyright (c) 2026 chencheng.
// Licensed under MIT; see THIRD_PARTY_NOTICES/Mayoi-LICENSE.

package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Commit is a commit of the history.
type CommitInfo struct {
	Hash    string
	Parents []string
	Author  string
	Email   string
	Time    time.Time
	Subject string
	Body    string // the message after the subject, trimmed
	Refs    []Ref  // the branches and tags at the commit
	// CoAuthors are those the message's Co-authored-by trailers name,
	// each once, in their order, the author left out.
	CoAuthors []Person
}

// Person is someone a commit names: their name and email.
type Person struct{ Name, Email string }

// parseCoAuthors reads the "Co-authored-by: Name <email>" lines of a
// commit's message, of any case, leaving out the author's and those
// named twice.
func parseCoAuthors(body, author, email string) []Person {
	var out []Person
	seen := map[string]bool{strings.ToLower(email): email != "", strings.ToLower(author): author != ""}
	for _, line := range strings.Split(body, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "Co-authored-by") {
			continue
		}
		v = strings.TrimSpace(v)
		var p Person
		if i := strings.LastIndex(v, "<"); i >= 0 && strings.HasSuffix(v, ">") {
			p = Person{Name: strings.TrimSpace(v[:i]), Email: strings.TrimSpace(v[i+1 : len(v)-1])}
		} else {
			p = Person{Name: v}
		}
		if p.Name == "" {
			p.Name = p.Email
		}
		if p.Name == "" {
			continue
		}
		key := strings.ToLower(cmp.Or(p.Email, p.Name))
		if seen[key] || seen[strings.ToLower(p.Name)] && p.Email == "" {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// RefKind is what a ref names.
type RefKind int

const (
	RefBranch RefKind = iota // a local branch
	RefRemote                // a branch of a remote, as "origin/main"
	RefTag
	RefHead // HEAD, detached at the commit
)

// Ref is a branch or tag at a commit: Head is set for the branch checked
// out.
type Ref struct {
	Name string
	Kind RefKind
	Head bool
}

// parseRefs reads the refs git's %D prints with --decorate=full, as
// "HEAD -> refs/heads/main, tag: refs/tags/v1, refs/remotes/origin/main",
// leaving out the HEADs of remotes.
func parseRefs(s string) []Ref {
	var refs []Ref
	for _, f := range strings.Split(s, ", ") {
		f = strings.TrimSpace(f)
		head := false
		if rest, ok := strings.CutPrefix(f, "HEAD -> "); ok {
			f, head = rest, true
		}
		f = strings.TrimPrefix(f, "tag: ")
		var r Ref
		switch {
		case f == "HEAD":
			r = Ref{Name: "HEAD", Kind: RefHead}
		case strings.HasPrefix(f, "refs/heads/"):
			r = Ref{Name: strings.TrimPrefix(f, "refs/heads/"), Kind: RefBranch, Head: head}
		case strings.HasPrefix(f, "refs/remotes/"):
			r = Ref{Name: strings.TrimPrefix(f, "refs/remotes/"), Kind: RefRemote}
			if strings.HasSuffix(r.Name, "/HEAD") {
				continue
			}
		case strings.HasPrefix(f, "refs/tags/"):
			r = Ref{Name: strings.TrimPrefix(f, "refs/tags/"), Kind: RefTag}
		default:
			continue
		}
		refs = append(refs, r)
	}
	return refs
}

// Short returns the first 7 characters of the commit's hash.
func (c CommitInfo) Short() string { return c.Hash[:min(7, len(c.Hash))] }

// commitFormat is how Log and CommitInfo have git print a commit: its
// fields, separated by unit separators.
const commitFormat = "--format=%H%x1f%P%x1f%an%x1f%ae%x1f%at%x1f%s%x1f%D%x1f%b"

func parseCommit(rec string) (CommitInfo, bool) {
	f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 8)
	if len(f) < 8 || f[0] == "" {
		return CommitInfo{}, false
	}
	c := CommitInfo{Hash: f[0], Parents: strings.Fields(f[1]), Author: f[2], Email: f[3], Subject: f[5],
		Refs: parseRefs(f[6]), Body: strings.TrimSpace(f[7])}
	c.CoAuthors = parseCoAuthors(c.Body, c.Author, c.Email)
	if s, err := strconv.ParseInt(f[4], 10, 64); err == nil {
		c.Time = time.Unix(s, 0)
	}
	return c, true
}

// ReadHistory rereads the published prefix, as Mayoi does. Appending a page
// never combines logs from different HEADs or ref snapshots.
func ReadHistory(ctx context.Context, root string, limit int, all bool) ([]CommitInfo, error) {
	refs := []string{"HEAD"}
	if all {
		refs = append(refs, "--branches", "--remotes", "--tags")
	}
	return readCommitLog(ctx, root, limit, 0, refs)
}
func readCommitLog(ctx context.Context, root string, limit, skip int, refs []string) ([]CommitInfo, error) {
	args := []string{"log", "-z", "--date-order", "--decorate=full", "--no-show-signature", fmt.Sprintf("-%d", max(1, limit)), fmt.Sprintf("--skip=%d", max(0, skip)), commitFormat}
	args = append(args, refs...)
	args = append(args, "--")
	out, err := run(ctx, root, args...)
	if err != nil {
		st, check := Read(ctx, root)
		if check == nil && st.Head == "(initial)" {
			return nil, nil
		}
		return nil, err
	}
	var cs []CommitInfo
	for _, rec := range strings.Split(string(out), "\x00") {
		if c, ok := parseCommit(rec); ok {
			cs = append(cs, c)
		}
	}
	return cs, nil
}
func ResolveCommit(ctx context.Context, root, rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", errors.New("提交引用无效")
	}
	out, err := run(ctx, root, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
	return strings.TrimSpace(string(out)), err
}
func UnpushedCommits(ctx context.Context, root string) (map[string]bool, error) {
	result := map[string]bool{}
	out, err := run(ctx, root, "rev-list", "HEAD", "--not", "--remotes", "--")
	if err != nil {
		return nil, err
	}
	for _, h := range strings.Fields(string(out)) {
		result[h] = true
	}
	return result, nil
}
func IsAncestor(ctx context.Context, root, from, to string) bool {
	_, err := run(ctx, root, "merge-base", "--is-ancestor", from, to)
	return err == nil
}
func comparisonEnds(value string) (string, string, bool) {
	from, to, ok := strings.Cut(value, "..")
	return from, to, ok && validHash(from) && validHash(to)
}
func CompareFiles(ctx context.Context, root, from, to string) ([]Entry, error) {
	if !validHash(from) || !validHash(to) {
		return nil, errors.New("比较提交无效")
	}
	out, err := run(ctx, root, "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", "--find-renames", from, to, "--")
	if err != nil {
		return nil, err
	}
	return parseCommitFiles(string(out))
}
func CompareDiff(ctx context.Context, root, from, to string, e Entry) (string, error) {
	if !validHash(from) || !validHash(to) {
		return "", errors.New("比较提交无效")
	}
	if err := validPaths([]string{e.Path}); err != nil {
		return "", err
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--find-renames", from, to, "--", e.Path}
	if e.Original != "" {
		if err := validPaths([]string{e.Original}); err != nil {
			return "", err
		}
		args = append(args, e.Original)
	}
	out, err := run(ctx, root, args...)
	return string(out), err
}

// CommitOperation acts on an immutable commit. Git enforces worktree ownership
// and preserves its stopped operation state for the existing conflict UI.
func CommitOperation(ctx context.Context, root, op, hash string, merge bool) error {
	if !validHash(hash) {
		return errors.New("提交 ID 无效")
	}
	var args []string
	switch op {
	case "checkout":
		args = []string{"switch", "--detach", hash}
	case "merge":
		args = []string{"merge", "--no-edit", hash}
	case "rebase":
		args = []string{"rebase", hash}
	case "cherry-pick", "revert":
		args = []string{op}
		if op == "revert" {
			args = append(args, "--no-edit")
		}
		if merge {
			args = append(args, "-m", "1")
		}
		args = append(args, hash)
	case "soft", "mixed", "hard":
		args = []string{"reset", "--" + op, hash}
	case "undo":
		head, err := ResolveCommit(ctx, root, "HEAD")
		if err != nil {
			return err
		}
		if head != hash {
			return errors.New("HEAD 已变化，请刷新后重试")
		}
		if _, err := ResolveCommit(ctx, root, "HEAD^"); err != nil {
			shallow, check := run(ctx, root, "rev-parse", "--is-shallow-repository")
			if check != nil || strings.TrimSpace(string(shallow)) != "false" {
				return errors.New("浅克隆边界提交不能撤销")
			}
			_, err = run(ctx, root, "update-ref", "-d", "HEAD", hash)
			return err
		}
		args = []string{"reset", "--soft", hash + "^"}
	default:
		return errors.New("未知提交操作")
	}
	_, err := run(ctx, root, args...)
	return err
}
func CreateRef(ctx context.Context, root, name, hash string, tag bool) error {
	if !validHash(hash) {
		return errors.New("提交 ID 无效")
	}
	name = strings.TrimSpace(name)
	prefix := "refs/heads/"
	if tag {
		prefix = "refs/tags/"
	}
	if name == "" || strings.HasPrefix(name, "-") {
		return errors.New("名称无效")
	}
	if _, err := run(ctx, root, "check-ref-format", prefix+name); err != nil {
		return err
	}
	command := "branch"
	if tag {
		command = "tag"
	}
	_, err := run(ctx, root, command, "--", name, hash)
	return err
}
func CommitPatch(ctx context.Context, root, hash string) (string, error) {
	if !validHash(hash) {
		return "", errors.New("提交 ID 无效")
	}
	parents, err := run(ctx, root, "rev-list", "--parents", "-n", "1", hash, "--")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(parents))
	args := []string{"diff-tree", "--no-color", "--root", "--no-commit-id", "--binary", "--no-ext-diff", "--no-textconv", "-p", "-r"}
	if len(fields) > 1 {
		args = append(args, fields[1])
	}
	args = append(args, hash, "--")
	out, err := run(ctx, root, args...)
	return string(out), err
}

// CommitStat is the first-parent delta shown in the hover card.
type CommitStat struct{ Files, Added, Deleted int }

func CommitStatistics(ctx context.Context, root string, c CommitInfo) (CommitStat, error) {
	if !validHash(c.Hash) {
		return CommitStat{}, errors.New("提交 ID 无效")
	}
	args := []string{"diff-tree", "-r", "-M", "--numstat", "--no-commit-id", "--no-ext-diff", "--no-textconv"}
	if len(c.Parents) == 0 {
		args = append(args, "--root", c.Hash)
	} else {
		if !validHash(c.Parents[0]) {
			return CommitStat{}, errors.New("父提交无效")
		}
		args = append(args, c.Parents[0], c.Hash)
	}
	args = append(args, "--")
	out, err := run(ctx, root, args...)
	if err != nil {
		return CommitStat{}, err
	}
	var stat CommitStat
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			continue
		}
		stat.Files++
		added, _ := strconv.Atoi(f[0])
		deleted, _ := strconv.Atoi(f[1])
		stat.Added += added
		stat.Deleted += deleted
	}
	return stat, nil
}
