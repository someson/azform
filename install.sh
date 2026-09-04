#!/bin/sh
# install.sh — single-command installer for azform (spec §14.3).
#
# Behaviour:
#   - Detect platform (darwin/linux × amd64/arm64).
#   - Download latest (or $AZFORM_VERSION-pinned) release tarball + checksum.
#   - Verify SHA256 against checksums.txt.
#   - Install binary to ~/.local/bin (or $AZFORM_BIN_DIR), mode 0755.
#   - Verify ~/.local/bin is on PATH; warn + suggest if not.
#   - Warn if `az` is missing (does NOT abort).
#   - Add widget source block to the active shell's profile between markers
#     (idempotent; backs up the profile; never overwrites a user-modified block).
#   - Print a one-screen summary with the next user action.
#
# --uninstall reverses the above (binary, share dir, profile block). State
# (drafts/bindings) is preserved unless --purge is given.
#
# POSIX sh compatible (dash on Debian).
set -eu

REPO="${AZFORM_REPO:-someson/azform}"
BIN_DIR="${AZFORM_BIN_DIR:-$HOME/.local/bin}"
SHARE_DIR="${AZFORM_SHARE_DIR:-$HOME/.local/share/azform}"
STATE_DIR="${AZFORM_STATE_DIR:-$HOME/.local/state/azform}"
VERSION="${AZFORM_VERSION:-}"

MARKER_BEGIN="# >>> azform >>>"
MARKER_END="# <<< azform <<<"
WIDGET_LINE="[ -f \"$SHARE_DIR/widget.zsh\" ] && source \"$SHARE_DIR/widget.zsh\""

log() { printf '%s\n' "$*"; }
err() { log "ERROR: $*" >&2; exit 1; }

detect_platform() {
    os=$(uname -s | tr 'A-Z' 'a-z')
    arch=$(uname -m)
    case "$arch" in
        x86_64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) err "unsupported architecture: $arch" ;;
    esac
    case "$os" in
        darwin|linux) ;;
        *) err "unsupported OS: $os" ;;
    esac
    printf '%s_%s' "$os" "$arch"
}

detect_shell() {
    case "${SHELL:-}" in
        */zsh) echo zsh ;;
        */bash) echo bash ;;
        *) echo sh ;;
    esac
}

profile_path() {
    shell=$(detect_shell)
    case "$shell" in
        zsh) echo "$HOME/.zshrc" ;;
        bash)
            if [ "${OS:-}" = "Windows_NT" ] || uname -s | grep -qi mingw; then
                echo "$HOME/.bash_profile"
            else
                echo "$HOME/.bashrc"
            fi
            ;;
        *) echo "$HOME/.profile" ;;
    esac
}

download() {
    url="$1"
    out="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$out" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$out" "$url"
    else
        err "neither curl nor wget found"
    fi
}

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        err "no SHA-256 tool found"
    fi
}

verify_checksum() {
    archive="$1"
    checksums="$2"
    archive_name=$(basename "$archive")
    expected=$(awk -v n="$archive_name" '$2 == n {print $1}' "$checksums")
    [ -n "$expected" ] || err "checksum for $archive_name not found"
    actual=$(sha256_of "$archive")
    [ "$expected" = "$actual" ] || err "checksum mismatch (expected $expected, got $actual)"
}

resolve_latest_version() {
    if [ -n "$VERSION" ]; then
        echo "$VERSION"
        return 0
    fi
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
            | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "https://api.github.com/repos/$REPO/releases/latest" \
            | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1
    else
        err "neither curl nor wget found"
    fi
}

install_binary() {
    platform=$1
    version=$2
    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT
    base="https://github.com/$REPO/releases/download/${version}"
    archive="azform_${version}_${platform}.tar.gz"
    download "$base/$archive" "$work/$archive"
    download "$base/checksums.txt" "$work/checksums.txt"
    verify_checksum "$work/$archive" "$work/checksums.txt"
    tar -xzf "$work/$archive" -C "$work"
    mkdir -p "$BIN_DIR"
    install -m 0755 "$work/azform" "$BIN_DIR/azform"
    mkdir -p "$SHARE_DIR"
}

write_widget() {
    mkdir -p "$SHARE_DIR"
    cat > "$SHARE_DIR/widget.zsh" <<'WIDGET'
# azform shell widget
azform-widget() {
  local out vars
  out=$(mktemp -t azform-out)
  vars=$(mktemp -t azform-vars)
  # Denylist: zsh built-in specials + prompt/theme noise. RANDOM intentionally kept.
  local -A azform_deny=(
    SECONDS 1 EPOCHSECONDS 1 EPOCHREALTIME 1
    UID 1 EUID 1 GID 1 EGID 1
    MATCH 1 MBEGIN 1 MEND 1 OPTARG 1 OPTIND 1
    HISTCHARS 1 histchars 1 HISTFILE 1 HISTSIZE 1 SAVEHIST 1
    LISTMAX 1 LOGCHECK 1 MAILCHECK 1
    MACHTYPE 1 CPUTYPE 1 OSTYPE 1 VENDOR 1
    HOST 1 HOSTNAME 1 SHORT_HOST 1 USERNAME 1
    LINES 1 COLUMNS 1 TTY 1 TMPPREFIX 1
    NULLCMD 1 READNULLCMD 1 WORDCHARS 1
    KEYTIMEOUT 1 KEYBOARD_HACK 1 FUNCNEST 1
    TRY_BLOCK_ERROR 1 TRY_BLOCK_INTERRUPT 1
    VCS_STATUS_RESULT 1 WATCH 1 ZLS_COLORS 1
  )
  local k
  for k in ${(k)parameters}; do
    case ${parameters[$k]} in
      scalar*|*integer*|*float*) ;;
      *) continue ;;
    esac
    [[ $k != RANDOM && ${parameters[$k]} == *(readonly|special)* ]] && continue
    [[ $k == _* || $k == POWERLEVEL9K_* || $k == P9K_* || $k == ZSH_* ]] && continue
    [[ $k == GITSTATUS_*_POWERLEVEL9K ]] && continue
    (( ${+azform_deny[$k]} )) && continue
    print -rn -- "$k=${(P)k}" >> "$vars"
    print -rn -- $'\0' >> "$vars"
  done
  azform --line "$BUFFER" --cursor "$CURSOR" --out "$out" --vars "$vars" --cwd "$PWD" </dev/tty >/dev/tty 2>&1
  if [[ -s "$out" ]]; then
    BUFFER=$(cat "$out")
    CURSOR=$#BUFFER
  fi
  rm -f "$out" "$vars"
  zle redisplay
}
zle -N azform-widget
bindkey '^Xa' azform-widget
WIDGET
}

add_to_profile() {
    prof=$(profile_path)
    [ -f "$prof" ] || : > "$prof"
    if grep -qF "$MARKER_BEGIN" "$prof"; then
        log "profile already contains azform block; leaving as-is"
        return 0
    fi
    backup="${prof}.azform.bak.$(date +%s)"
    cp "$prof" "$backup"
    {
        printf '\n%s\n%s\n%s\n' "$MARKER_BEGIN" "$WIDGET_LINE" "$MARKER_END"
    } >> "$prof"
    log "added azform block to $prof (backup: $backup)"
}

uninstall() {
    rm -f "$BIN_DIR/azform"
    rm -rf "$SHARE_DIR"
    if [ "${PURGE_STATE:-0}" = "1" ]; then
        rm -rf "$STATE_DIR"
    fi
    for prof in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
        if [ -f "$prof" ] && grep -qF "$MARKER_BEGIN" "$prof"; then
            backup="${prof}.azform.uninst.$(date +%s)"
            cp "$prof" "$backup"
            awk -v b="$MARKER_BEGIN" -v e="$MARKER_END" '
                $0==b {skip=1; next}
                $0==e {skip=0; next}
                !skip {print}
            ' "$prof" > "$prof.tmp" && mv "$prof.tmp" "$prof"
            log "removed azform block from $prof (backup: $backup)"
        fi
    done
    log "uninstalled"
}

case "${1:-}" in
    --uninstall)
        uninstall
        exit 0
        ;;
esac

platform=$(detect_platform)
version=$(resolve_latest_version)
[ -n "$version" ] || err "could not determine latest version"

install_binary "$platform" "$version"
write_widget
add_to_profile
prof=$(profile_path)

case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) log "WARNING: $BIN_DIR is not in PATH; add it before using azform" ;;
esac

command -v az >/dev/null 2>&1 || log "WARNING: az not found; install Azure CLI before using azform"

cat <<SUMMARY

azform $version installed.

  binary:  $BIN_DIR/azform
  widget:  $SHARE_DIR/widget.zsh
  profile: $prof

Restart the shell or run:  exec "$SHELL"

SUMMARY
