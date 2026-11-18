// Portions adapted from Mayoi, Copyright (c) 2026 chencheng.
// Licensed under MIT; see THIRD_PARTY_NOTICES/Mayoi-LICENSE.
package git

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// RemoteWebURL maps known public hosting services to credential-free HTTPS
// links. hash is a commit ID, never a branch name or a URL path.
func RemoteWebURL(rawURL, hash string) (string, error) {
	if !validHash(hash) {
		return "", fmt.Errorf("invalid commit ID")
	}
	host, path, err := remoteRepo(rawURL)
	if err != nil {
		return "", err
	}
	segment := "/commit/"
	switch host {
	case "gitlab.com":
		segment = "/-" + segment
	case "bitbucket.org":
		segment = "/commits/"
	}
	return (&url.URL{Scheme: "https", Host: host, Path: path + segment + hash}).String(), nil
}

// remoteRepo returns the host of a known public hosting service a remote
// URL is of, and the repository's path there, without credentials.
func remoteRepo(rawURL string) (host, path string, err error) {
	if !strings.Contains(rawURL, "://") {
		left, right, ok := strings.Cut(rawURL, ":")
		if !ok || !strings.Contains(left, "@") {
			return "", "", fmt.Errorf("unsupported remote URL")
		}
		rawURL = "ssh://" + left + "/" + right
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid remote URL")
	}
	if u.Scheme != "https" && u.Scheme != "ssh" {
		return "", "", fmt.Errorf("unsupported remote URL scheme")
	}
	host = strings.ToLower(u.Hostname())
	if host != "github.com" && host != "gitlab.com" && host != "bitbucket.org" {
		return "", "", fmt.Errorf("unsupported remote hosting service")
	}
	path = strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repository path")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", "", fmt.Errorf("invalid repository path")
		}
	}
	return host, path, nil
}

// CommitRemoteURL prefers a unique decoration, the current upstream, origin,
// then the first remote. Only the chosen remote's fetch URL is read.
func CommitRemoteURL(ctx context.Context, root string, cm CommitInfo) (string, error) {
	names, err := Remotes(ctx, root)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", fmt.Errorf("this repository has no remote")
	}
	decorated := map[string]bool{}
	for _, ref := range cm.Refs {
		if ref.Kind != RefRemote {
			continue
		}
		match := ""
		for _, name := range names {
			if strings.HasPrefix(ref.Name, name+"/") && len(name) > len(match) {
				match = name
			}
		}
		if match != "" {
			decorated[match] = true
		}
	}
	chosen := ""
	if len(decorated) == 1 {
		for name := range decorated {
			chosen = name
		}
	}
	raw, _, err := commitRemoteURL(ctx, root, names, chosen)
	if err != nil {
		return "", err
	}
	return RemoteWebURL(raw, cm.Hash)
}

// remoteURL reads the fetch URL of chosen, else of the current upstream's
// remote, origin, then the first remote; and the upstream's branch on its
// remote, "" when the branch has none there.
func commitRemoteURL(ctx context.Context, root string, names []string, chosen string) (raw, upstream string, err error) {
	out, err := run(ctx, root, "for-each-ref", "--format=%(HEAD)%1f%(upstream:remotename)%1f%(upstream:remoteref)", "refs/heads")
	if err != nil {
		return "", "", err
	}
	upRemote := ""
	for _, row := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(row, "*\x1f"); ok {
			name, ref, _ := strings.Cut(rest, "\x1f")
			if slices.Contains(names, name) {
				upRemote = name
				upstream = strings.TrimPrefix(ref, "refs/heads/")
			}
		}
	}
	if chosen == "" {
		chosen = upRemote
	}
	if chosen == "" {
		chosen = names[0]
		if slices.Contains(names, "origin") {
			chosen = "origin"
		}
	}
	if chosen != upRemote {
		upstream = ""
	}
	out, err = run(ctx, root, "remote", "get-url", "--", chosen)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSuffix(string(out), "\n"), upstream, nil
}
