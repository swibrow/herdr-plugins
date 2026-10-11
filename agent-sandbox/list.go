package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// list is the picker pane: enter reattaches in a new tab, ctrl-x deletes.
func list(ctx context.Context, cfg config) error {
	k, err := newKube(cfg)
	if err != nil {
		return err
	}
	claims, err := k.listClaims(ctx)
	if err != nil {
		return err
	}
	if len(claims) == 0 {
		pause("no sandboxes; press enter to close")
		return nil
	}
	var lines []string
	for _, c := range claims {
		src := claimSource(&c)
		ref := src.Ref
		if ref == "" {
			ref = "(default)"
		}
		age := time.Since(c.GetCreationTimestamp().Time).Round(time.Minute)
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", c.GetName(), src.Repo, ref, age))
	}

	fzf := exec.Command("fzf", "--expect", "ctrl-x", "--delimiter", "\t",
		"--border-label", " sandboxes ", "--header", "enter: attach in new tab / ctrl-x: delete")
	fzf.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	fzf.Stderr = os.Stderr
	out, err := fzf.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 130 {
		return nil
	}
	if err != nil {
		return err
	}
	parts := strings.SplitN(strings.TrimRight(string(out), "\n"), "\n", 2)
	if len(parts) < 2 {
		return nil
	}
	name, _, _ := strings.Cut(parts[1], "\t")
	if parts[0] == "ctrl-x" {
		return k.deleteClaim(ctx, name)
	}
	return openPane("session", "tab", []string{"SANDBOX_CLAIM=" + name})
}
