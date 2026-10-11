package main

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type source struct {
	Repo    string
	Ref     string
	Warning string
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func gitSource(dir string) (source, error) {
	remote, err := git(dir, "remote", "get-url", "origin")
	if err != nil {
		return source{}, fmt.Errorf("%s has no origin remote", dir)
	}
	repo, err := parseRemote(remote)
	if err != nil {
		return source{}, err
	}
	src := source{Repo: repo}
	branch, err := git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "HEAD" {
		src.Warning = "detached HEAD; the sandbox will use the default branch"
		return src, nil
	}
	src.Ref = branch
	if _, err := git(dir, "rev-parse", "--abbrev-ref", "@{u}"); err != nil {
		src.Warning = fmt.Sprintf("branch %s has no upstream; the sandbox will use the default branch", branch)
		src.Ref = ""
	} else if ahead, _ := git(dir, "rev-list", "--count", "@{u}..HEAD"); ahead != "0" {
		src.Warning = fmt.Sprintf("%s local commits on %s are not pushed and won't be in the sandbox", ahead, branch)
	}
	if dirty, _ := git(dir, "status", "--porcelain"); dirty != "" && src.Warning == "" {
		src.Warning = "uncommitted changes won't be in the sandbox"
	}
	return src, nil
}

// parseRemote turns a GitHub remote URL into owner/repo.
func parseRemote(remote string) (string, error) {
	path := ""
	switch {
	case strings.HasPrefix(remote, "git@github.com:"):
		path = strings.TrimPrefix(remote, "git@github.com:")
	default:
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() != "github.com" {
			return "", fmt.Errorf("remote %q is not a GitHub repo", remote)
		}
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("remote %q is not a GitHub repo", remote)
	}
	return path, nil
}
