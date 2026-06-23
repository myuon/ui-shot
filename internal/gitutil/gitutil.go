// Package gitutil resolves repo and commit information from a git checkout.
package gitutil

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// runner allows tests to stub git invocation.
var runner = func(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Commit returns the current HEAD commit SHA.
func Commit() (string, error) {
	sha, err := runner("rev-parse", "HEAD")
	if err != nil || sha == "" {
		return "", fmt.Errorf("could not resolve commit (run inside a git repo or pass --commit)")
	}
	return sha, nil
}

// Repo returns owner/repo inferred from the origin remote URL.
func Repo() (string, error) {
	url, err := runner("config", "--get", "remote.origin.url")
	if err != nil || url == "" {
		return "", fmt.Errorf("could not resolve repo from git remote (pass --repo owner/repo)")
	}
	repo := ParseRemoteURL(url)
	if repo == "" {
		return "", fmt.Errorf("could not parse owner/repo from remote URL %q (pass --repo owner/repo)", url)
	}
	return repo, nil
}

var sshLike = regexp.MustCompile(`^[\w.-]+@([\w.-]+):(.+)$`)

// ParseRemoteURL extracts owner/repo from common git remote URL forms:
//
//	https://github.com/owner/repo.git
//	git@github.com:owner/repo.git
//	ssh://git@github.com/owner/repo.git
func ParseRemoteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	path := ""

	switch {
	case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "ssh://"), strings.HasPrefix(raw, "git://"):
		// Strip scheme and host: take everything after the first '/' that
		// follows the host.
		rest := raw
		for _, p := range []string{"http://", "https://", "ssh://", "git://"} {
			rest = strings.TrimPrefix(rest, p)
		}
		// rest = host[:port]/owner/repo...
		if idx := strings.Index(rest, "/"); idx >= 0 {
			path = rest[idx+1:]
		}
	default:
		if m := sshLike.FindStringSubmatch(raw); m != nil {
			path = m[2]
		}
	}

	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimSuffix(path, "/")

	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}
	owner := parts[len(parts)-2]
	repo := parts[len(parts)-1]
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}
