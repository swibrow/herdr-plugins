#!/usr/bin/env bash
# Runs inside the claimed pod (ghcr.io/cloudsnacks/claude-code). The image's
# entrypoint already pointed git at GITHUB_TOKEN when the pod started.
set -euo pipefail

repo=$1
ref=${2:-}
dir=/workspace/${repo#*/}
config=${CLAUDE_CONFIG_DIR:-$HOME/.claude}

mkdir -p "$config"
[[ -f $config/.claude.json ]] || printf '%s' '{"hasCompletedOnboarding":true,"theme":"dark"}' > "$config/.claude.json"

if [[ ! -d $dir/.git ]]; then
  git clone "https://github.com/$repo.git" "$dir"
  if [[ -n $ref ]] && ! git -C "$dir" checkout "$ref"; then
    echo "ref $ref not found on the remote; staying on the default branch"
    sleep 2
  fi
fi

cd "$dir"

# Under tmux a dropped connection leaves claude running and `new-session -A`
# reattaches. Exit 3 tells the plugin tmux was detached, not ended.
if command -v tmux >/dev/null; then
  export TERM=${TERM:-xterm-256color}
  # shellcheck disable=SC2086
  tmux new-session -A -s claude claude ${CLAUDE_ARGS:-} \; set -g status off \; set -g mouse on \; set -s escape-time 0 || true
  if tmux has-session -t claude 2>/dev/null; then
    exit 3
  fi
  exit 0
fi

# Without tmux, a reconnect resumes the conversation instead.
if [[ -f .git/herdr-session ]]; then
  # shellcheck disable=SC2086
  exec claude --continue ${CLAUDE_ARGS:-}
fi
touch .git/herdr-session
# shellcheck disable=SC2086
exec claude ${CLAUDE_ARGS:-}
