# azform shell widget. Source this from your .zshrc:
#   [ -f "$HOME/.local/share/azform/widget.zsh" ] && source "$HOME/.local/share/azform/widget.zsh"
#
# The widget dumps every non-readonly scalar parameter in your shell
# (both exported and shell-local — zsh's ${(k)parameters} covers both)
# to a NUL-separated file azform reads on startup. It also creates
# an --env-out file azform writes to on Done; after azform exits,
# the widget evals every line in your interactive zsh so vars queued
# via the g-popup land in your session until you unset them.
azform-widget() {
  local out vars env
  out=$(mktemp -t azform-out)
  vars=$(mktemp -t azform-vars)
  env=$(mktemp -t azform-env)
  # Denylist: zsh built-in specials + prompt/theme noise. RANDOM intentionally kept.
  local -A azform_deny=(
    SECONDS 1 EPOCHSECONDS 1 EPOCHREALTIME 1
    UID 1 EUID 1 GID 1 EGID 1
    MATCH 1 MBEGIN 1 MEND 1 OPTARG 1 OPTIND 1
    HISTCHARS 1 histchars 1 HISTFILE 1 HISTSIZE 1 SAVEHIST 1
    LISTMAX 1 LOGCHECK 1 MAILCHECK 2
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
  # DEBUG: log widget invocation so the user can verify which
  # widget version is loaded and whether the eval block runs.
  # Set AZFORM_WIDGET_DEBUG=1 to enable. Writes to
  # /tmp/azform-widget.log in append mode; the file is created on
  # the first call. Safe to leave installed — costs nothing when
  # the var is unset.
  if [[ -n "${AZFORM_WIDGET_DEBUG:-}" ]]; then
    {
      print -r -- "=== azform-widget invoked at $(date '+%H:%M:%S') ==="
      print -r -- "  out=$out"
      print -r -- "  vars=$vars"
      print -r -- "  env=$env"
      print -r -- "  azform=$commands[azform]"
      print -r -- "  BUFFER=${(qq)BUFFER}"
    } >> /tmp/azform-widget.log
  fi
  azform --line "$BUFFER" --cursor "$CURSOR" --out "$out" --vars "$vars" --env-out "$env" --cwd "$PWD" </dev/tty >/dev/tty 2>&1
  local buf_after=""
  if [[ -s "$out" ]]; then
    BUFFER=$(cat "$out")
    CURSOR=$#BUFFER
    buf_after=$BUFFER
  fi
  if [[ -n "${AZFORM_WIDGET_DEBUG:-}" ]]; then
    {
      print -r -- "  after azform exit:"
      print -r -- "    out size: $(wc -c < "$out" 2>/dev/null || echo missing) bytes"
      print -r -- "    env size: $(wc -c < "$env" 2>/dev/null || echo missing) bytes"
      if [[ -s "$env" ]]; then
        print -r -- "    env content:"
        print -r -- "$(cat "$env")"
      fi
      if [[ -n "$buf_after" ]]; then
        print -r -- "    DONE pressed (buffer updated)"
      else
        print -r -- "    CANCEL/empty (buffer unchanged → azform wrote no command)"
      fi
    } >> /tmp/azform-widget.log
  fi
  # Apply any shell variables the user queued via the g-popup. Each line
  # is `NAME='value'` produced by azform itself, so eval is safe; we
  # deliberately don't `source` so we never accidentally execute
  # arbitrary shell from disk. The var lives in your interactive zsh
  # session until you unset it (no `export` — shell-local, not
  # inherited by subprocesses).
  #
  # `typeset -g` forces the assignment into the global scope. In a vanilla
  # zsh, plain assignments from inside a function already leak to the
  # global table for new var names, but a user's setopt (TYPESET_TO_UNDEF,
  # LOCAL_OPTIONS, etc.) or a wrapping function in a sourced module can
  # change that — using typeset -g makes the behaviour identical
  # regardless of the caller's environment.
  if [[ -s "$env" ]]; then
    while IFS= read -r line; do
      eval "typeset -g $line" || eval "$line"
    done < "$env"
  fi
  if [[ -n "${AZFORM_WIDGET_DEBUG:-}" ]]; then
    {
      print -r -- "  after eval: newVar type=${(t)newVar} value=$newVar"
    } >> /tmp/azform-widget.log
  fi
  rm -f "$out" "$vars" "$env"
  zle redisplay
}
zle -N azform-widget
bindkey '^Xa' azform-widget