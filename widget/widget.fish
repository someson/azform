# azform shell widget for fish 3.4+. Source this from your config.fish:
#   test -f "$HOME/.local/share/azform/widget.fish"; and source "$HOME/.local/share/azform/widget.fish"
#
# fish counterpart of widget.zsh / widget.bash. Kept as a separate file
# for the same reason those two are: fish shares no syntax with them, and
# its introspection (`set -n`, `commandline`) has nothing in common with
# zsh's ${(k)parameters} or bash's READLINE_LINE.
#
# Differences the Go side accounts for via `--shell fish`:
#   - fish single quotes escape only \\ and \' (POSIX has no escapes), so
#     the assembled command is quoted with fish rules;
#   - there is no NAME=VALUE assignment, so --env-out holds
#     `set -g NAME 'value'` lines instead.

# fish 3.4 introduced $(...) command substitution, which azform emits
# and parses; older fish would render commands it cannot run.
if not status is-interactive
    return 0
end
set -l __azform_v (string split . -- $version)
if test $__azform_v[1] -lt 3; or begin
        test $__azform_v[1] -eq 3; and test $__azform_v[2] -lt 4
    end
    echo "azform: widget needs fish 3.4+ (found $version); keybinding not installed" >&2
    return 0
end

# __azform_fish_dump_vars writes every plain shell variable to $argv[1]
# as NUL-separated NAME=VALUE records — the format vars.ReadFile parses
# for the zsh and bash widgets too.
#
# fish variables are lists; a list is flattened with a space, except the
# PATH-style ones, which fish itself joins with ':' when exporting.
#
# NOTE: this denylist is intentionally not the one in widget.zsh or
# widget.bash — fish's special variables differ. Do not "sync" them.
function __azform_fish_dump_vars
    set -l file $argv[1]
    begin
        # -ng/-nU: only global and universal variables. Plain `set -n`
        # would also list this function's own locals (file, name, …).
        for name in (begin; set -ng; set -nU; end | sort -u)
            switch $name
                case 'fish_*' '__*' '_*' POWERLEVEL9K_'*' 'P9K_*'
                    continue
                case status pipestatus argv history version hostname umask last_pid \
                    CMD_DURATION COLUMNS LINES SHLVL FISH_VERSION OLDPWD \
                    UID EUID GID EGID PPID IFS \
                    dirprev dirnext dirstack
                    continue
            end
            if string match -q -- '*PATH' $name
                printf '%s=%s\0' $name (string join ':' -- $$name)
            else
                printf '%s=%s\0' $name (string join ' ' -- $$name)
            end
        end
    end >$file
end

function azform-widget
    set -l tmp /tmp
    set -q TMPDIR; and test -n "$TMPDIR"; and set tmp $TMPDIR
    # Explicit template rather than `mktemp -t`: BSD and GNU mktemp
    # disagree on what -t means; this form is correct on both.
    set -l out (mktemp "$tmp/azform-out.XXXXXX")
    set -l vars (mktemp "$tmp/azform-vars.XXXXXX")
    set -l envf (mktemp "$tmp/azform-env.XXXXXX")

    __azform_fish_dump_vars $vars

    # --cursor-prefix: `commandline -C` counts characters but azform
    # needs a byte offset, so the text left of the cursor is passed and
    # its byte length is exact.
    set -l buf (commandline -b | string collect)
    set -l cur (commandline -C)
    set -l prefix (string sub -l $cur -- $buf | string collect)

    azform --shell fish --line "$buf" --cursor $cur --cursor-prefix "$prefix" \
        --out $out --vars $vars --env-out $envf --cwd $PWD \
        </dev/tty >/dev/tty 2>&1

    if test -s $out
        set -l result (string collect <$out)
        commandline -r -- $result
        commandline -C (string length -- $result)
    end

    # Apply variables queued via the g-popup. Each line is
    # `set -g NAME 'value'`, written by azform itself with fish quoting,
    # so eval is safe; the file is never `source`d.
    if test -s $envf
        while read -l line
            eval $line
        end <$envf
    end

    # Test hook: preserve env-out at a stable path so the e2e test can
    # read it after the temp files are removed. No-op when unset.
    if set -q AZFORM_ENV_OUT_KEEP; and test -n "$AZFORM_ENV_OUT_KEEP"
        cp $envf $AZFORM_ENV_OUT_KEEP 2>/dev/null
    end

    rm -f $out $vars $envf
    commandline -f repaint
end

bind \cxa azform-widget
bind -M insert \cxa azform-widget 2>/dev/null
