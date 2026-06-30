#!/bin/sh
# loradex installer — portable, single-folder install.
#
# Everything loradex owns lives under one home folder ($LORADEX_HOME, default
# ~/.loradex): the binary (bin/), config, credentials, downloaded base models
# (models/), and trainer backends (trainers/). Override the home with --home DIR
# or the LORADEX_HOME env var.
#
# Usage:
#   ./install.sh [--home DIR]
#   curl -fsSL https://raw.githubusercontent.com/keeandrews/loradex-cli/main/install.sh | sh
set -eu

REPO_URL="https://github.com/keeandrews/loradex-cli"
GO_PKG="github.com/keeandrews/loradex-cli"
DEFAULT_HOME="${HOME}/.loradex"

info() { printf '  %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

# --- args ---
LORADEX_HOME="${LORADEX_HOME:-$DEFAULT_HOME}"
while [ $# -gt 0 ]; do
  case "$1" in
    --home)   LORADEX_HOME="${2:?--home needs a directory}"; shift 2 ;;
    --home=*) LORADEX_HOME="${1#*=}"; shift ;;
    -h|--help) printf 'usage: install.sh [--home DIR]\n'; exit 0 ;;
    *) warn "ignoring unknown arg: $1"; shift ;;
  esac
done

# Normalize to an absolute path.
case "$LORADEX_HOME" in
  /*) : ;;
  *)  LORADEX_HOME="$(CDPATH= cd -- "$(dirname -- "$LORADEX_HOME")" 2>/dev/null && pwd)/$(basename -- "$LORADEX_HOME")" || LORADEX_HOME="$PWD/$LORADEX_HOME" ;;
esac
export LORADEX_HOME

BIN_DIR="$LORADEX_HOME/bin"
BIN="$BIN_DIR/loradex"

printf '\nloradex installer\n'
info "home: $LORADEX_HOME"
info "os:   $(uname -s) $(uname -m)"

# run_uninstall removes ALL loradex files. Prefers the binary's own `uninstall`
# (cleans the shell profile + both home dirs precisely); falls back to rm.
run_uninstall() {
  if [ -x "$BIN" ] && "$BIN" uninstall --yes 2>/dev/null; then
    return 0
  fi
  warn "falling back to a direct removal of $LORADEX_HOME"
  rm -rf "$LORADEX_HOME"
}

# --- already installed? offer reinstall / uninstall / quit ---
existing=""
[ -x "$BIN" ] && existing="$BIN"
[ -z "$existing" ] && [ -f "$LORADEX_HOME/config.yaml" ] && existing="$LORADEX_HOME"
if [ -n "$existing" ]; then
  printf '\nAn existing loradex install was found:\n  %s\n' "$existing"
  if [ -t 0 ]; then
    printf '\n[R]einstall (remove everything, then install fresh)\n'
    printf '[U]ninstall (remove everything and quit)\n'
    printf '[Q]uit (leave it as-is)\n'
    printf 'Choose [R/u/q]: '
    read -r ans || ans="q"
  else
    ans="q"
  fi
  case "$(printf '%s' "${ans:-r}" | tr 'A-Z' 'a-z')" in
    r|reinstall|"") info "removing the existing install…"; run_uninstall ;;
    u|uninstall)
      info "removing the existing install…"; run_uninstall
      printf '\nUninstalled. Open a new terminal to clear any stale PATH/LORADEX_HOME.\n'
      exit 0 ;;
    *) info "quit — left the existing install untouched."; exit 0 ;;
  esac
fi

mkdir -p "$BIN_DIR"

# --- obtain the binary ---
build_from_repo() {
  [ -d "$SCRIPT_DIR/cli" ] || return 1
  command -v go >/dev/null 2>&1 || return 1
  info "building from source ($SCRIPT_DIR/cli)…"
  ( cd "$SCRIPT_DIR/cli" && go build -trimpath -o "$BIN" . )
}

install_via_go() {
  command -v go >/dev/null 2>&1 || return 1
  info "go install $GO_PKG@latest…"
  GOBIN="$BIN_DIR" go install "$GO_PKG@latest" || return 1
  # `go install` names the binary after the package ("cli"); normalize it.
  [ -x "$BIN_DIR/cli" ] && mv -f "$BIN_DIR/cli" "$BIN"
  [ -x "$BIN" ]
}

if build_from_repo; then
  :
elif install_via_go; then
  :
else
  die "could not build loradex. Install Go 1.26+ (https://go.dev/dl) and re-run from a loradex checkout."
fi
[ -x "$BIN" ] || die "build did not produce $BIN"
info "binary: $BIN"

# --- trainer selection + PATH (delegated to the wizard, sharing this home) ---
printf '\n'
"$BIN" setup || warn "setup did not complete — you can re-run: $BIN setup"

# --- refresh the shell so `loradex` is immediately usable ---
printf '\nInstalled. '
if [ -t 0 ] && [ -t 1 ] && [ -n "${SHELL:-}" ]; then
  printf 'Starting a fresh shell so loradex is on your PATH…\n'
  exec "$SHELL" -l
else
  printf 'Open a new terminal (or source your shell profile), then run: loradex\n'
fi
