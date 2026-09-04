#!/bin/sh
# Smoke test: dry-run the install.sh profile-marker logic against a fake
# HOME so we don't pollute the real ~/.zshrc. Does NOT download anything.
set -eu
TMPHOME=$(mktemp -d)
export HOME="$TMPHOME"
export AZFORM_BIN_DIR="$TMPHOME/bin"
export AZFORM_SHARE_DIR="$TMPHOME/share"
export AZFORM_STATE_DIR="$TMPHOME/state"

mkdir -p "$TMPHOME"
touch "$HOME/.zshrc"
echo "before install" > "$HOME/.zshrc"

# Manually invoke the relevant helper functions from install.sh.
MARKER_BEGIN="# >>> azform >>>"
MARKER_END="# <<< azform <<<"
SHARE_DIR="$AZFORM_SHARE_DIR"
WIDGET_LINE="[ -f \"$SHARE_DIR/widget.zsh\" ] && source \"$SHARE_DIR/widget.zsh\""

add_to_profile() {
    prof=$1
    [ -f "$prof" ] || : > "$prof"
    if grep -qF "$MARKER_BEGIN" "$prof"; then
        echo "already has marker" >&2
        return 0
    fi
    backup="${prof}.azform.bak.$(date +%s)"
    cp "$prof" "$backup"
    printf '\n%s\n%s\n%s\n' "$MARKER_BEGIN" "$WIDGET_LINE" "$MARKER_END" >> "$prof"
}

# First install — should add block.
add_to_profile "$HOME/.zshrc"
grep -qF "$MARKER_BEGIN" "$HOME/.zshrc" || { echo "FAIL: marker not added"; exit 1; }
echo "OK: marker added"

# Second install — should be idempotent.
SIZE1=$(wc -c < "$HOME/.zshrc")
add_to_profile "$HOME/.zshrc"
SIZE2=$(wc -c < "$HOME/.zshrc")
[ "$SIZE1" = "$SIZE2" ] || { echo "FAIL: profile grew on re-install"; exit 1; }
echo "OK: idempotent"

# Verify block is between markers and has the widget source.
awk '
    /^# >>> azform >>>/ { in_block = 1; print "block start" }
    in_block { print }
    /^# <<< azform <<</ { in_block = 0; print "block end" }
' "$HOME/.zshrc" | grep -q "widget.zsh" || { echo "FAIL: widget source not in block"; exit 1; }
echo "OK: block contents"

# Uninstall — strip the block.
awk -v b="$MARKER_BEGIN" -v e="$MARKER_END" '
    $0==b {skip=1; next}
    $0==e {skip=0; next}
    !skip {print}
' "$HOME/.zshrc" > "$HOME/.zshrc.tmp" && mv "$HOME/.zshrc.tmp" "$HOME/.zshrc"
grep -qF "$MARKER_BEGIN" "$HOME/.zshrc" && { echo "FAIL: marker not removed"; exit 1; }
echo "OK: uninstall removes marker"

echo "smoke test passed"
rm -rf "$TMPHOME"
