package main

import (
	"regexp"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseRemote(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:swibrow/home-ops.git":       "swibrow/home-ops",
		"https://github.com/swibrow/home-ops":       "swibrow/home-ops",
		"https://github.com/swibrow/home-ops.git/":  "swibrow/home-ops",
		"ssh://git@github.com/swibrow/home-ops.git": "swibrow/home-ops",
	} {
		got, err := parseRemote(remote)
		if err != nil || got != want {
			t.Errorf("parseRemote(%q) = %q, %v; want %q", remote, got, err, want)
		}
	}
	for _, remote := range []string{"https://gitlab.com/a/b.git", "git@github.com:onlyowner", "/local/path"} {
		if got, err := parseRemote(remote); err == nil {
			t.Errorf("parseRemote(%q) = %q; want error", remote, got)
		}
	}
}

func TestClaimName(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	for _, repo := range []string{"swibrow/home-ops", "Owner/My_Repo.js", "o/a-very-long-repository-name-that-goes-on-and-on-forever"} {
		name := claimName(repo)
		if len(name) > 63 || !valid.MatchString(name) {
			t.Errorf("claimName(%q) = %q is not a valid name", repo, name)
		}
	}
}

func TestNewClaim(t *testing.T) {
	src := source{Repo: "swibrow/home-ops", Ref: "main"}
	c := newClaim("claude-x", "claude", src, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	pool, _, _ := unstructured.NestedString(c.Object, "spec", "warmPoolRef", "name")
	shutdown, _, _ := unstructured.NestedString(c.Object, "spec", "lifecycle", "shutdownTime")
	if pool != "claude" || shutdown != "2026-01-02T03:04:05Z" {
		t.Errorf("pool=%q shutdown=%q", pool, shutdown)
	}
	if got := claimSource(c); got != src {
		t.Errorf("claimSource = %+v, want %+v", got, src)
	}
}

func TestEventPaneID(t *testing.T) {
	for raw, want := range map[string]string{
		`{"type":"pane_closed","pane_id":"w1:p2","workspace_id":"w1"}`:            "w1:p2",
		`{"event":"pane.closed","data":{"type":"pane_closed","pane_id":"w1:p3"}}`: "w1:p3",
		``: "",
	} {
		if got := eventPaneID(raw); got != want {
			t.Errorf("eventPaneID(%q) = %q, want %q", raw, got, want)
		}
	}
}
