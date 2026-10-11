package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Pane-to-claim mapping, so the pane.closed handler knows which claim to delete.
func paneFile(pane string) string {
	return filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "panes", strings.ReplaceAll(pane, ":", "_"))
}

func trackPane(pane, claim string) error {
	if pane == "" || os.Getenv("HERDR_PLUGIN_STATE_DIR") == "" {
		return nil
	}
	f := paneFile(pane)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(claim), 0o644)
}

func trackedClaim(pane string) (string, bool) {
	if os.Getenv("HERDR_PLUGIN_STATE_DIR") == "" {
		return "", false
	}
	b, err := os.ReadFile(paneFile(pane))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func untrackPane(pane string) {
	if pane != "" && os.Getenv("HERDR_PLUGIN_STATE_DIR") != "" {
		os.Remove(paneFile(pane))
	}
}
