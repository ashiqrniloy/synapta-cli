#!/usr/bin/env sh
set -eu

REPO="${REPO:-synapta/synapta-cli}"
BINARY="synapta"

log() {
  printf '%s\n' "$*"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    log "error: required command not found: $1"
    exit 1
  }
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *)
      log "error: unsupported OS: $(uname -s)"
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      log "error: unsupported architecture: $(uname -m)"
      exit 1
      ;;
  esac
}

json_get_tag() {
  if command -v jq >/dev/null 2>&1; then
    jq -r '.tag_name'
  else
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
  fi
}

install_dir() {
  if [ -n "${SYNAPTA_INSTALL_DIR:-}" ]; then
    echo "$SYNAPTA_INSTALL_DIR"
    return
  fi

  if [ -w "/usr/local/bin" ]; then
    echo "/usr/local/bin"
  else
    echo "$HOME/.local/bin"
  fi
}

main() {
  need_cmd curl
  need_cmd tar

  OS="$(detect_os)"
  ARCH="$(detect_arch)"

  API_URL="https://api.github.com/repos/$REPO/releases/latest"
  TAG="$(curl -fsSL "$API_URL" | json_get_tag)"
  if [ -z "$TAG" ] || [ "$TAG" = "null" ]; then
    log "error: could not determine latest release tag from $API_URL"
    exit 1
  fi

  VERSION_NO_V="${TAG#v}"
  ASSET="${BINARY}_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
  DOWNLOAD_URL="https://github.com/$REPO/releases/download/$TAG/$ASSET"

  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

  log "Downloading $ASSET ..."
  curl -fL "$DOWNLOAD_URL" -o "$TMP_DIR/$ASSET"

  tar -xzf "$TMP_DIR/$ASSET" -C "$TMP_DIR"

  TARGET_DIR="$(install_dir)"
  mkdir -p "$TARGET_DIR"

  if [ -w "$TARGET_DIR" ]; then
    install -m 0755 "$TMP_DIR/$BINARY" "$TARGET_DIR/$BINARY"
  else
    need_cmd sudo
    sudo install -m 0755 "$TMP_DIR/$BINARY" "$TARGET_DIR/$BINARY"
  fi

  log "Installed $BINARY $TAG to $TARGET_DIR/$BINARY"

  case ":$PATH:" in
    *":$TARGET_DIR:"*) ;;
    *)
      log ""
      log "Add $TARGET_DIR to your PATH, e.g.:"
      log "  export PATH=\"$TARGET_DIR:\$PATH\""
      ;;
  esac

  log ""
  log "Run: $BINARY version"
}

main "$@"
