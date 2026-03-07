#!/bin/sh
# helpers.sh — idempotent shell primitives for Anchor modules.
#
# Source this at the top of a module's apply block:
#   . /usr/lib/anchor/helpers.sh
#
# Call helpers to make changes, then call anchor_exit at the end.
# Exit code will be 0 (no changes) or 80 (changes were made).
#
# Functions:
#   anchor_file <source> <dest>
#       Install source file to dest. Only copies if contents differ.
#       Creates parent directories as needed.
#
#   anchor_link <target> <linkpath>
#       Ensure a symlink at linkpath points to target.
#       Errors if linkpath exists and is not a symlink.
#       Creates parent directories as needed.
#
#   anchor_line <file> <line>
#       Ensure the exact line exists in file (matched with grep -xF).
#       Appends if missing. Creates the file and parent directories
#       if they don't exist.
#
#   anchor_dir <path> [mode]
#       Ensure directory exists. Optionally enforce a permission mode
#       (e.g. 0755). Creates parent directories as needed.
#
#   anchor_chmod <mode> <path>
#       Ensure file or directory has the given permission mode.
#
#   anchor_chown <owner> <path>
#       Ensure file or directory has the given ownership.
#       Owner can be "user", "user:group", or ":group".
#       Accepts both symbolic names and numeric IDs.
#
#   anchor_absent <path>
#       Ensure a file, directory, or symlink does not exist.
#       Removes recursively if it's a directory.
#
#   anchor_exit
#       Call at the end of your apply block. Exits with code 0
#       if no helpers made changes, or 80 if any did.

_ANCHOR_CHANGED=0

# Mark that a change was made. Called internally by helpers.
_anchor_mark_changed() {
    _ANCHOR_CHANGED=1
}

# anchor_file installs a file.
anchor_file() {
    _src="$1"
    _dest="$2"

    if [ -z "$_src" ] || [ -z "$_dest" ]; then
        echo "anchor_file: requires <source> <dest>" >&2
        return 1
    fi

    if [ ! -f "$_src" ]; then
        echo "anchor_file: source not found: $_src" >&2
        return 1
    fi

    if [ -f "$_dest" ] && cmp -s "$_src" "$_dest"; then
        return 0
    fi

    _parent=$(dirname "$_dest")
    if [ ! -d "$_parent" ]; then
        mkdir -p "$_parent"
    fi

    cp "$_src" "$_dest"
    echo "installed $_dest"
    _anchor_mark_changed
}

# anchor_link ensures a symlink.
anchor_link() {
    _target="$1"
    _link="$2"

    if [ -z "$_target" ] || [ -z "$_link" ]; then
        echo "anchor_link: requires <target> <linkpath>" >&2
        return 1
    fi

    if [ -L "$_link" ]; then
        _current=$(readlink "$_link")
        if [ "$_current" = "$_target" ]; then
            return 0
        fi
        rm "$_link"
    elif [ -e "$_link" ]; then
        echo "anchor_link: $_link exists and is not a symlink" >&2
        return 1
    fi

    _parent=$(dirname "$_link")
    if [ ! -d "$_parent" ]; then
        mkdir -p "$_parent"
    fi

    ln -s "$_target" "$_link"
    echo "linked $_link -> $_target"
    _anchor_mark_changed
}

# anchor_line ensures a line exists in a file.
anchor_line() {
    _file="$1"
    _line="$2"

    if [ -z "$_file" ] || [ -z "$_line" ]; then
        echo "anchor_line: requires <file> <line>" >&2
        return 1
    fi

    if [ -f "$_file" ] && grep -qxF "$_line" "$_file"; then
        return 0
    fi

    _parent=$(dirname "$_file")
    if [ ! -d "$_parent" ]; then
        mkdir -p "$_parent"
    fi

    echo "$_line" >> "$_file"
    echo "added line to $_file"
    _anchor_mark_changed
}

# anchor_dir ensures a directory exists.
anchor_dir() {
    _path="$1"
    _mode="${2:-}"

    if [ -z "$_path" ]; then
        echo "anchor_dir: requires <path>" >&2
        return 1
    fi

    if [ -d "$_path" ]; then
        if [ -n "$_mode" ]; then
            _current=$(stat -c '%04a' "$_path" 2>/dev/null) || _current=$(stat -f '%04Lp' "$_path" 2>/dev/null)
            if [ "$_current" != "$_mode" ]; then
                chmod "$_mode" "$_path"
                echo "chmod $_mode $_path"
                _anchor_mark_changed
            fi
        fi
        return 0
    fi

    if [ -n "$_mode" ]; then
        mkdir -p "$_path"
        chmod "$_mode" "$_path"
    else
        mkdir -p "$_path"
    fi
    echo "created $_path"
    _anchor_mark_changed
}

# anchor_chmod ensures a file has the given permission mode.
anchor_chmod() {
    _mode="$1"
    _path="$2"

    if [ -z "$_mode" ] || [ -z "$_path" ]; then
        echo "anchor_chmod: requires <mode> <path>" >&2
        return 1
    fi

    if [ ! -e "$_path" ]; then
        echo "anchor_chmod: path not found: $_path" >&2
        return 1
    fi

    _current=$(stat -c '%04a' "$_path" 2>/dev/null) || _current=$(stat -f '%04Lp' "$_path" 2>/dev/null)
    if [ "$_current" = "$_mode" ]; then
        return 0
    fi

    chmod "$_mode" "$_path"
    echo "chmod $_mode $_path"
    _anchor_mark_changed
}

# anchor_chown ensures a file has the given ownership.
anchor_chown() {
    _owner="$1"
    _path="$2"

    if [ -z "$_owner" ] || [ -z "$_path" ]; then
        echo "anchor_chown: requires <owner> <path>" >&2
        return 1
    fi

    if [ ! -e "$_path" ]; then
        echo "anchor_chown: path not found: $_path" >&2
        return 1
    fi

    # Determine what to compare based on the owner spec format.
    case "$_owner" in
        :*)
            # :group — only check group
            _current_sym=":$(stat -c '%G' "$_path" 2>/dev/null)" || _current_sym=":$(stat -f '%Sg' "$_path" 2>/dev/null)"
            _current_num=":$(stat -c '%g' "$_path" 2>/dev/null)" || _current_num=":$(stat -f '%g' "$_path" 2>/dev/null)"
            ;;
        *:*)
            # user:group
            _current_sym=$(stat -c '%U:%G' "$_path" 2>/dev/null) || _current_sym=$(stat -f '%Su:%Sg' "$_path" 2>/dev/null)
            _current_num=$(stat -c '%u:%g' "$_path" 2>/dev/null) || _current_num=$(stat -f '%u:%g' "$_path" 2>/dev/null)
            ;;
        *)
            # user only
            _current_sym=$(stat -c '%U' "$_path" 2>/dev/null) || _current_sym=$(stat -f '%Su' "$_path" 2>/dev/null)
            _current_num=$(stat -c '%u' "$_path" 2>/dev/null) || _current_num=$(stat -f '%u' "$_path" 2>/dev/null)
            ;;
    esac

    if [ "$_current_sym" = "$_owner" ] || [ "$_current_num" = "$_owner" ]; then
        return 0
    fi

    chown "$_owner" "$_path"
    echo "chown $_owner $_path"
    _anchor_mark_changed
}

# anchor_absent ensures a path does not exist.
anchor_absent() {
    _path="$1"

    if [ -z "$_path" ]; then
        echo "anchor_absent: requires <path>" >&2
        return 1
    fi

    if [ ! -e "$_path" ] && [ ! -L "$_path" ]; then
        return 0
    fi

    rm -rf "$_path"
    echo "removed $_path"
    _anchor_mark_changed
}

# anchor_exit exits with 0 (no changes) or 80 (changes made).
anchor_exit() {
    if [ "$_ANCHOR_CHANGED" = "1" ]; then
        exit 80
    fi
    exit 0
}
