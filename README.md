# herdr-plugins

Plugins for [herdr](https://herdr.dev).

| Plugin | What it does |
|---|---|
| [`agent-sandbox`](agent-sandbox) | Opens Claude Code in an [agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) pod on a fresh clone of the focused repo |

## agent-sandbox

The `launch` action reads the focused pane's directory, resolves its GitHub `origin` and current branch, and opens a new tab. That tab:

1. Creates a `SandboxClaim` against the `claude` warm pool, with a 12h `shutdownTime` backstop.
2. Waits for the claimed pod to be ready.
3. Execs [`session.sh`](agent-sandbox/session.sh) in the pod. It clones the repo into `/workspace/<repo>`, checks out the branch and runs `claude`.

What ends the session:

| Event | Result |
|---|---|
| `claude` exits | The claim is deleted |
| The tab is closed | The `pane.closed` hook deletes the claim |
| The connection drops | The tab offers to reconnect, which resumes with `claude --continue` |

The `list` action opens a picker of running sandboxes: `enter` reattaches in a new tab and `ctrl-x` deletes.

Only what is pushed reaches the sandbox. Unpushed commits, uncommitted changes and branches without an upstream produce a warning.

### Install

```sh
herdr plugin install swibrow/herdr-plugins/agent-sandbox   # builds with go
# or, for development
cd agent-sandbox && go build -o bin/herdr-sandbox . && herdr plugin link "$PWD"
```

Bind keys in `~/.config/herdr/config.toml` (any free keys):

```toml
[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "swibrow.agent-sandbox.launch"
description = "Claude in sandbox"

[[keys.command]]
key = "prefix+shift+a"
type = "plugin_action"
command = "swibrow.agent-sandbox.list"
description = "Sandboxes"
```

### Configuration

Optional `config.json` in `herdr plugin config-dir swibrow.agent-sandbox`:

```json
{ "context": "admin@pitower", "namespace": "herdr-sandbox", "pool": "claude", "ttl": "12h" }
```

The values shown are the defaults. Cluster access uses your kubeconfig.

### Cluster side

The `SandboxTemplate`, `SandboxWarmPool` and `ExternalSecret` live in home-ops under `kubernetes/apps/pitower/herdr-sandbox/claude`. Pods run [`ghcr.io/cloudsnacks/claude-code`](https://github.com/cloudsnacks/containers/tree/main/images/claude-code) with `args: [sleep, infinity]`. That keeps its entrypoint, which configures git auth from `GITHUB_TOKEN`.

## Development

```sh
cd agent-sandbox
go vet ./... && go test ./...
herdr plugin log list --plugin swibrow.agent-sandbox   # action and hook output
```
