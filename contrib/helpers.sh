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
    _anchor_src="$1"
    _anchor_dest="$2"

    if [ -z "$_anchor_src" ] || [ -z "$_anchor_dest" ]; then
        echo "anchor_file: requires <source> <dest>" >&2
        return 1
    fi

    if [ ! -f "$_anchor_src" ]; then
        echo "anchor_file: source not found: $_anchor_src" >&2
        return 1
    fi

    if [ -f "$_anchor_dest" ] && cmp -s "$_anchor_src" "$_anchor_dest"; then
        return 0
    fi

    _anchor_parent=$(dirname "$_anchor_dest")
    if [ ! -d "$_anchor_parent" ]; then
        mkdir -p "$_anchor_parent"
    fi

    cp "$_anchor_src" "$_anchor_dest"
    echo "installed $_anchor_dest"
    _anchor_mark_changed
}

# anchor_link ensures a symlink.
anchor_link() {
    _anchor_target="$1"
    _anchor_linkpath="$2"

    if [ -z "$_anchor_target" ] || [ -z "$_anchor_linkpath" ]; then
        echo "anchor_link: requires <target> <linkpath>" >&2
        return 1
    fi

    if [ -L "$_anchor_linkpath" ]; then
        _anchor_current=$(readlink "$_anchor_linkpath")
        if [ "$_anchor_current" = "$_anchor_target" ]; then
            return 0
        fi
        rm "$_anchor_linkpath"
    elif [ -e "$_anchor_linkpath" ]; then
        echo "anchor_link: $_anchor_linkpath exists and is not a symlink" >&2
        return 1
    fi

    _anchor_parent=$(dirname "$_anchor_linkpath")
    if [ ! -d "$_anchor_parent" ]; then
        mkdir -p "$_anchor_parent"
    fi

    ln -s "$_anchor_target" "$_anchor_linkpath"
    echo "linked $_anchor_linkpath -> $_anchor_target"
    _anchor_mark_changed
}

# anchor_line ensures a line exists in a file.
anchor_line() {
    _anchor_file="$1"
    _anchor_line="$2"

    if [ -z "$_anchor_file" ] || [ -z "$_anchor_line" ]; then
        echo "anchor_line: requires <file> <line>" >&2
        return 1
    fi

    if [ -f "$_anchor_file" ] && grep -qxF "$_anchor_line" "$_anchor_file"; then
        return 0
    fi

    _anchor_parent=$(dirname "$_anchor_file")
    if [ ! -d "$_anchor_parent" ]; then
        mkdir -p "$_anchor_parent"
    fi

    echo "$_anchor_line" >> "$_anchor_file"
    echo "added line to $_anchor_file"
    _anchor_mark_changed
}

# anchor_dir ensures a directory exists.
anchor_dir() {
    _anchor_path="$1"
    _anchor_mode="${2:-}"

    if [ -z "$_anchor_path" ]; then
        echo "anchor_dir: requires <path>" >&2
        return 1
    fi

    if [ -d "$_anchor_path" ]; then
        if [ -n "$_anchor_mode" ]; then
            _anchor_current=$(stat -c '%04a' "$_anchor_path" 2>/dev/null) || _anchor_current=$(stat -f '%04Lp' "$_anchor_path" 2>/dev/null)
            if [ "$_anchor_current" != "$_anchor_mode" ]; then
                chmod "$_anchor_mode" "$_anchor_path"
                echo "chmod $_anchor_mode $_anchor_path"
                _anchor_mark_changed
            fi
        fi
        return 0
    fi

    if [ -n "$_anchor_mode" ]; then
        mkdir -p "$_anchor_path"
        chmod "$_anchor_mode" "$_anchor_path"
    else
        mkdir -p "$_anchor_path"
    fi
    echo "created $_anchor_path"
    _anchor_mark_changed
}

# anchor_chmod ensures a file has the given permission mode.
anchor_chmod() {
    _anchor_mode="$1"
    _anchor_path="$2"

    if [ -z "$_anchor_mode" ] || [ -z "$_anchor_path" ]; then
        echo "anchor_chmod: requires <mode> <path>" >&2
        return 1
    fi

    if [ ! -e "$_anchor_path" ]; then
        echo "anchor_chmod: path not found: $_anchor_path" >&2
        return 1
    fi

    _anchor_current=$(stat -c '%04a' "$_anchor_path" 2>/dev/null) || _anchor_current=$(stat -f '%04Lp' "$_anchor_path" 2>/dev/null)
    if [ "$_anchor_current" = "$_anchor_mode" ]; then
        return 0
    fi

    chmod "$_anchor_mode" "$_anchor_path"
    echo "chmod $_anchor_mode $_anchor_path"
    _anchor_mark_changed
}

# anchor_chown ensures a file has the given ownership.
anchor_chown() {
    _anchor_owner="$1"
    _anchor_path="$2"

    if [ -z "$_anchor_owner" ] || [ -z "$_anchor_path" ]; then
        echo "anchor_chown: requires <owner> <path>" >&2
        return 1
    fi

    if [ ! -e "$_anchor_path" ]; then
        echo "anchor_chown: path not found: $_anchor_path" >&2
        return 1
    fi

    # Determine what to compare based on the owner spec format.
    case "$_anchor_owner" in
        :*)
            # :group — only check group
            _anchor_current_sym=":$(stat -c '%G' "$_anchor_path" 2>/dev/null)" || _anchor_current_sym=":$(stat -f '%Sg' "$_anchor_path" 2>/dev/null)"
            _anchor_current_num=":$(stat -c '%g' "$_anchor_path" 2>/dev/null)" || _anchor_current_num=":$(stat -f '%g' "$_anchor_path" 2>/dev/null)"
            ;;
        *:*)
            # user:group
            _anchor_current_sym=$(stat -c '%U:%G' "$_anchor_path" 2>/dev/null) || _anchor_current_sym=$(stat -f '%Su:%Sg' "$_anchor_path" 2>/dev/null)
            _anchor_current_num=$(stat -c '%u:%g' "$_anchor_path" 2>/dev/null) || _anchor_current_num=$(stat -f '%u:%g' "$_anchor_path" 2>/dev/null)
            ;;
        *)
            # user only
            _anchor_current_sym=$(stat -c '%U' "$_anchor_path" 2>/dev/null) || _anchor_current_sym=$(stat -f '%Su' "$_anchor_path" 2>/dev/null)
            _anchor_current_num=$(stat -c '%u' "$_anchor_path" 2>/dev/null) || _anchor_current_num=$(stat -f '%u' "$_anchor_path" 2>/dev/null)
            ;;
    esac

    if [ "$_anchor_current_sym" = "$_anchor_owner" ] || [ "$_anchor_current_num" = "$_anchor_owner" ]; then
        return 0
    fi

    chown "$_anchor_owner" "$_anchor_path"
    echo "chown $_anchor_owner $_anchor_path"
    _anchor_mark_changed
}

# anchor_absent ensures a path does not exist.
anchor_absent() {
    _anchor_path="$1"

    if [ -z "$_anchor_path" ]; then
        echo "anchor_absent: requires <path>" >&2
        return 1
    fi

    if [ ! -e "$_anchor_path" ] && [ ! -L "$_anchor_path" ]; then
        return 0
    fi

    rm -rf "$_anchor_path"
    echo "removed $_anchor_path"
    _anchor_mark_changed
}

# anchor_exit exits with 0 (no changes) or 80 (changes made).
anchor_exit() {
    if [ "$_ANCHOR_CHANGED" = "1" ]; then
        exit 80
    fi
    exit 0
}
