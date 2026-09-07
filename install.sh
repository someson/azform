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

# bash_major echoes the major version of the *login* shell's bash.
# It must interrogate "$SHELL" specifically: $SHELL, `bash` on PATH and
# a package-manager bash can be three different binaries at three
# different versions (macOS ships 3.2 as /bin/bash while Homebrew
# installs 5.x elsewhere). Echoes 0 when it cannot tell, so an
# unreadable version is treated as unsupported rather than assumed new.
bash_major() {
    "${SHELL:-/bin/bash}" -c 'echo "${BASH_VERSINFO[0]}"' 2>/dev/null || echo 0
}

# azform_widget_for_shell echoes the widget filename for a shell and a
# bash major version, or nothing at all when the shell cannot host the
# widget. bash < 4 lacks READLINE_LINE/READLINE_POINT, so the binding
# would fire and silently do nothing; sh and dash have no keybinding
# mechanism whatsoever.
azform_widget_for_shell() {
    case "$1" in
        zsh) echo widget.zsh ;;
        bash)
            if [ "${2:-0}" -ge 4 ] 2>/dev/null; then
                echo widget.bash
            fi
            ;;
        *) : ;;
    esac
}

# azform_unsupported_message echoes the explanation a user gets when
# their shell cannot host the widget. This message is the entire
# product for those users, so the wording is part of the contract.
# It deliberately never names a package manager: bash 3.x is a version
# problem, not a macOS problem.
azform_unsupported_message() {
    if [ "$1" = bash ]; then
        printf "widget not installed: your bash is %s.x; azform's widget needs bash 4+. Upgrade bash, then re-run this installer.\n" "${2:-?}"
    else
        printf "widget not installed: your shell is %s; azform's widget supports zsh and bash 4+. Re-run from your target shell.\n" "$1"
    fi
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

# POSIX sh has no local variables, so every name assigned here is global.
# Names are vc_-prefixed to keep the function from clobbering its caller:
# an earlier version assigned plain `archive`, which overwrote
# install_binary's own `archive` and produced "$work/$work/…" on the next
# line. Keep the prefix when editing.
verify_checksum() {
    vc_archive="$1"
    vc_checksums="$2"
    vc_name=$(basename "$vc_archive")
    vc_expected=$(awk -v n="$vc_name" '$2 == n {print $1}' "$vc_checksums")
    [ -n "$vc_expected" ] || err "checksum for $vc_name not found"
    vc_actual=$(sha256_of "$vc_archive")
    [ "$vc_expected" = "$vc_actual" ] || err "checksum mismatch (expected $vc_expected, got $vc_actual)"
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

# azform_archive_name echoes the release archive filename for a version
# and platform. The tag keeps its leading "v" (v0.1.1) but goreleaser's
# name_template uses {{ .Version }}, which does not — so the "v" is
# stripped here. Without this every download 404s.
azform_archive_name() {
    printf 'azform_%s_%s.tar.gz' "${1#v}" "$2"
}

install_binary() {
    platform=$1
    version=$2
    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT
    base="https://github.com/$REPO/releases/download/${version}"
    archive=$(azform_archive_name "$version" "$platform")
    download "$base/$archive" "$work/$archive"
    download "$base/checksums.txt" "$work/checksums.txt"
    verify_checksum "$work/$archive" "$work/checksums.txt"
    tar -xzf "$work/$archive" -C "$work"
    mkdir -p "$BIN_DIR"
    install -m 0755 "$work/azform" "$BIN_DIR/azform"
    mkdir -p "$SHARE_DIR"
}

# write_widget asks the freshly installed binary for its widgets
# instead of copying them from next to this script. When the installer
# is piped (`curl … | sh`) there is no script directory to copy from —
# $0 is "sh" — so a copy-based install worked only inside a repo
# checkout. Emitting from the binary also makes widget/binary skew
# impossible: the widget is whatever that binary was built with.
write_widget() {
    mkdir -p "$SHARE_DIR"
    # Both widgets are installed regardless of the current shell: it
    # costs nothing and means switching shells later works without
    # re-running the installer.
    for shell in zsh bash; do
        tmp="$SHARE_DIR/widget.$shell.tmp.$$"
        # Via a temp file: a failing binary must not leave a truncated
        # widget.$shell behind for the profile to source.
        "$BIN_DIR/azform" shell-init "$shell" > "$tmp" \
            || err "azform shell-init $shell failed"
        mv "$tmp" "$SHARE_DIR/widget.$shell"
    done
}

add_to_profile() {
    shell=$(detect_shell)
    major=0
    [ "$shell" = bash ] && major=$(bash_major)
    widget=$(azform_widget_for_shell "$shell" "$major")

    # No widget for this shell: install nothing into the profile and
    # say why. Writing a block that can never work is the bug this
    # branch exists to prevent.
    if [ -z "$widget" ]; then
        log "$(azform_unsupported_message "$shell" "$major")"
        return 0
    fi

    prof=$(profile_path)
    [ -f "$prof" ] || : > "$prof"
    if grep -qF "$MARKER_BEGIN" "$prof"; then
        log "profile already contains azform block; leaving as-is"
        return 0
    fi
    backup="${prof}.azform.bak.$(date +%s)"
    cp "$prof" "$backup"
    widget_line="[ -f \"$SHARE_DIR/$widget\" ] && source \"$SHARE_DIR/$widget\""
    {
        printf '\n%s\n%s\n%s\n' "$MARKER_BEGIN" "$widget_line" "$MARKER_END"
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

# Test seam: sourced with AZFORM_INSTALL_LIB=1, define the functions
# above and stop. Lets the shell-selection logic be unit tested without
# downloading a release or writing to a real profile.
if [ "${AZFORM_INSTALL_LIB:-0}" = "1" ]; then
    return 0 2>/dev/null || exit 0
fi

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
  widget:  $SHARE_DIR/widget.zsh, $SHARE_DIR/widget.bash
  profile: $prof

Restart the shell or run:  exec "$SHELL"

SUMMARY
