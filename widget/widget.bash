# azform shell widget for bash 4+. Source this from your .bashrc:
#   [ -f "$HOME/.local/share/azform/widget.bash" ] && source "$HOME/.local/share/azform/widget.bash"
#
# bash counterpart of widget.zsh. The two files are deliberately kept
# separate rather than merged behind a `case $SHELL`: the line-editing
# and variable-introspection primitives have no common subset between
# the shells, so a single file would be harder to read than either half.

# azform_bash_dump_vars writes every plain scalar shell variable to $1
# as NUL-separated NAME=VALUE records — the format vars.ReadFile parses
# for the zsh widget too.
#
# Type filtering reproduces what zsh's ${(k)parameters} gives for free.
# `declare -p NAME` prints `declare -<attrs> NAME=...`, so the attribute
# field tells us to skip arrays (a), associative arrays (A) and readonly
# (r). Readonly matters: PPID, UID and friends are readonly scalars that
# would otherwise show up in the variable picker as noise.
#
# NOTE: this denylist is intentionally NOT the same as the one in
# widget.zsh — bash's special parameters differ from zsh's. Do not
# "sync" the two lists; each is correct for its own shell.
azform_bash_dump_vars() {
  local out=$1
  : > "$out"
  local k attr
  for k in $(compgen -v); do
    case $k in
      BASH_*|COMP_*|FUNCNAME|PIPESTATUS|DIRSTACK|GROUPS|LINENO|SECONDS) continue ;;
      _*|POWERLEVEL9K_*|P9K_*) continue ;;
      COLUMNS|LINES|HISTFILE|HISTSIZE|HISTFILESIZE|HISTCONTROL) continue ;;
      OLDPWD|PS1|PS2|PS3|PS4|PROMPT_COMMAND) continue ;;
    esac
    attr=$(declare -p "$k" 2>/dev/null | awk '{print $2}')
    case $attr in
      *a*|*A*|*r*) continue ;;
    esac
    printf '%s=%s\0' "$k" "${!k}" >> "$out"
  done
}

# Guard. Two conditions, both necessary:
#   - interactive: `bind -x` is meaningless in a non-interactive shell,
#     and sourcing this file from a script must stay a silent no-op.
#   - bash >= 4: READLINE_LINE/READLINE_POINT arrived in 4.0. On 3.x the
#     binding fires and silently does nothing — the line is never
#     rewritten and no error is printed, which is a worse failure than
#     not binding at all.
#
# install.sh normally declines to write the profile block for bash < 4,
# so this branch is a backstop for the paths the installer cannot see:
# hand-sourcing, a profile copied between machines, or a bash downgrade
# after install. It is not the primary gate.
if [[ $- != *i* ]]; then
  return 0 2>/dev/null || true
fi
if (( ${BASH_VERSINFO[0]:-0} < 4 )); then
  printf 'azform: widget needs bash 4.0+ (found %s); keybinding not installed\n' \
    "${BASH_VERSION:-unknown}" >&2
  return 0 2>/dev/null || true
fi

azform-widget() {
  local out vars env line
  # Explicit template rather than `mktemp -t azform-out`: BSD mktemp
  # (macOS) treats -t's argument as a prefix and appends its own random
  # suffix, but GNU coreutils (Linux) requires the template to contain
  # at least three X's and errors with "too few X's in template",
  # leaving the variable empty and every redirect below writing to "".
  # This form is correct on both.
  out=$(mktemp "${TMPDIR:-/tmp}/azform-out.XXXXXX")
  vars=$(mktemp "${TMPDIR:-/tmp}/azform-vars.XXXXXX")
  env=$(mktemp "${TMPDIR:-/tmp}/azform-env.XXXXXX")

  azform_bash_dump_vars "$vars"

  azform --line "$READLINE_LINE" --cursor "$READLINE_POINT" \
         --out "$out" --vars "$vars" --env-out "$env" --cwd "$PWD" \
         </dev/tty >/dev/tty 2>&1

  if [[ -s "$out" ]]; then
    READLINE_LINE=$(cat "$out")
    READLINE_POINT=${#READLINE_LINE}
  fi

  # Apply variables queued via the g-popup. Each line is NAME='value'
  # produced by azform's quoteForShell, so eval is safe; we never
  # `source` the file, so arbitrary shell from disk is never executed.
  #
  # `declare -g` forces the assignment into the global scope, but it is
  # bash 4.2+ while our floor is 4.0 — hence the fallback. A plain
  # assignment inside a function already targets the global scope
  # unless a `local` of the same name shadows it.
  if [[ -s "$env" ]]; then
    while IFS= read -r line; do
      eval "declare -g $line" || eval "$line"
    done < "$env"
  fi

  # Test hook: preserve env-out at a stable path so the e2e test can
  # read it after the temp files are cleaned up. No-op when unset,
  # same pattern as AZFORM_WIDGET_DEBUG in widget.zsh.
  if [[ -n "${AZFORM_ENV_OUT_KEEP:-}" ]]; then
    cp "$env" "$AZFORM_ENV_OUT_KEEP" 2>/dev/null || true
  fi

  rm -f "$out" "$vars" "$env"
}

bind -x '"\C-xa": azform-widget'
