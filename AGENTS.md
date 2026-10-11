# herdr-plugins

One herdr plugin per subdirectory, each with its own `herdr-plugin.toml`.

## agent-sandbox

- Go module in `agent-sandbox/`. Verify with `go vet ./... && go test ./...`.
- `herdr plugin link` skips `[[build]]`, so run `go build -o bin/herdr-sandbox .` after changes.
- herdr actions and event hooks run with no visible terminal: their output only shows up in `herdr plugin log list`. Anything that needs to report errors to the user belongs in the `session` pane (`attach`), not in `launch`.
- `session.sh` is embedded in the binary and exec'd as `bash -c <script> session.sh <repo> <ref>`. Repo and ref are exec args, not claim env, because warm-pool pods are already running when they are claimed. The image's own `AGENT_REPO` clone only runs at container start, so the plugin can't use it.
- The image has no tmux. A reconnect resumes the conversation with `claude --continue`, keyed off the `.git/herdr-session` marker.
- SandboxClaim pod labels must use a domain on the controller's allowlist, so this plugin labels only the claim object itself.
- The image comes from cloudsnacks/containers (`images/claude-code`). It runs as UID 1001, and the template's securityContext must match.
