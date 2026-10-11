package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const pluginID = "swibrow.agent-sandbox"

type config struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Pool      string `json:"pool"`
	TTL       string `json:"ttl"`
}

func loadConfig() (config, error) {
	cfg := config{Context: "admin@pitower", Namespace: "herdr-sandbox", Pool: "claude", TTL: "12h"}
	dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if dir == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config.json: %w", err)
	}
	return cfg, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: herdr-sandbox launch|attach|reap|picker|list")
		os.Exit(2)
	}
	cfg, err := loadConfig()
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = run(ctx, cfg, os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herdr-sandbox:", err)
		if os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "" {
			pause("press enter to close")
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config, cmd string) error {
	switch cmd {
	case "launch":
		return launch()
	case "picker":
		return openPane("picker", "", nil)
	case "attach":
		return attach(ctx, cfg)
	case "reap":
		return reap(ctx, cfg)
	case "list":
		return list(ctx, cfg)
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// launch runs as a plugin action, where output is not visible, so it only
// resolves the focused directory and hands everything else to the session pane.
func launch() error {
	cwd, err := focusedCwd()
	if err != nil {
		return err
	}
	return openPane("session", "tab", []string{"SANDBOX_CWD=" + cwd})
}

func herdrBin() string {
	if p := os.Getenv("HERDR_BIN_PATH"); p != "" {
		return p
	}
	return "herdr"
}

func herdr(args ...string) ([]byte, error) {
	out, err := exec.Command(herdrBin(), args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("herdr %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

type paneInfo struct {
	PaneID        string `json:"pane_id"`
	Focused       bool   `json:"focused"`
	Cwd           string `json:"cwd"`
	ForegroundCwd string `json:"foreground_cwd"`
}

func (p paneInfo) dir() string {
	if p.ForegroundCwd != "" {
		return p.ForegroundCwd
	}
	return p.Cwd
}

func focusedCwd() (string, error) {
	if id := os.Getenv("HERDR_PANE_ID"); id != "" {
		out, err := herdr("pane", "get", id)
		if err != nil {
			return "", err
		}
		var resp struct {
			Result struct {
				Pane paneInfo `json:"pane"`
			} `json:"result"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return "", err
		}
		return resp.Result.Pane.dir(), nil
	}
	out, err := herdr("pane", "list")
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Panes []paneInfo `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", err
	}
	for _, p := range resp.Result.Panes {
		if p.Focused {
			return p.dir(), nil
		}
	}
	return "", errors.New("no focused pane")
}

func openPane(entrypoint, placement string, env []string) error {
	args := []string{"plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", entrypoint, "--focus"}
	if placement != "" {
		args = append(args, "--placement", placement)
	}
	for _, e := range env {
		args = append(args, "--env", e)
	}
	_, err := herdr(args...)
	return err
}

func attach(ctx context.Context, cfg config) error {
	k, err := newKube(cfg)
	if err != nil {
		return err
	}

	name := os.Getenv("SANDBOX_CLAIM")
	if name == "" {
		cwd := os.Getenv("SANDBOX_CWD")
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		src, err := gitSource(cwd)
		if err != nil {
			return err
		}
		if src.Warning != "" {
			fmt.Println("warning:", src.Warning)
		}
		ttl, err := time.ParseDuration(cfg.TTL)
		if err != nil {
			return fmt.Errorf("ttl: %w", err)
		}
		name, err = k.createClaim(ctx, src, time.Now().Add(ttl))
		if err != nil {
			return err
		}
		fmt.Printf("claimed %s/%s for %s@%s\n", cfg.Namespace, name, src.Repo, src.Ref)
	}

	pane := os.Getenv("HERDR_PANE_ID")
	if err := trackPane(pane, name); err != nil {
		return err
	}

	for {
		src, pod, err := k.waitForPod(ctx, name)
		if err != nil {
			return err
		}
		code, err := k.execSession(ctx, pod, src)
		switch {
		case err == nil && code == 0:
			untrackPane(pane)
			return k.deleteClaim(context.Background(), name)
		case ctx.Err() != nil:
			return nil
		}
		fmt.Printf("\r\nsession ended (exit %d, %v)\r\n", code, err)
		if !confirm("reconnect and resume? [Y/n] ") {
			untrackPane(pane)
			return k.deleteClaim(context.Background(), name)
		}
	}
}

func reap(ctx context.Context, cfg config) error {
	pane := eventPaneID(os.Getenv("HERDR_PLUGIN_EVENT_JSON"))
	if pane == "" {
		return nil
	}
	name, ok := trackedClaim(pane)
	if !ok {
		return nil
	}
	k, err := newKube(cfg)
	if err != nil {
		return err
	}
	if err := k.deleteClaim(ctx, name); err != nil {
		return err
	}
	untrackPane(pane)
	return nil
}

// eventPaneID accepts the payload either bare or wrapped in an event envelope.
func eventPaneID(raw string) string {
	var ev struct {
		PaneID string `json:"pane_id"`
		Data   struct {
			PaneID string `json:"pane_id"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &ev) != nil {
		return ""
	}
	if ev.PaneID != "" {
		return ev.PaneID
	}
	return ev.Data.PaneID
}

func pause(prompt string) {
	fmt.Print(prompt)
	var b [1]byte
	os.Stdin.Read(b[:])
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	var line string
	fmt.Scanln(&line)
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "" || line == "y" || line == "yes"
}
