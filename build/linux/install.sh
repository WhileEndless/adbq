#!/bin/sh
# Installs adbq for the current user: the binary on PATH, plus the desktop entry
# and icon where the desktop environment looks for them.
#
# Per-user by default (no sudo, nothing outside $HOME). Pass a prefix to install
# somewhere else, e.g. `sudo ./install.sh /usr/local` for all users.
#
# Uninstall is the reverse and just as boring:
#   rm ~/.local/bin/adbq ~/.local/share/applications/adbq.desktop \
#      ~/.local/share/icons/hicolor/512x512/apps/adbq.png
set -eu

prefix="${1:-$HOME/.local}"
here="$(cd "$(dirname "$0")" && pwd)"

mkdir -p "$prefix/bin" \
         "$prefix/share/applications" \
         "$prefix/share/icons/hicolor/512x512/apps"

install -m 0755 "$here/adbq" "$prefix/bin/adbq"
install -m 0644 "$here/share/applications/adbq.desktop" "$prefix/share/applications/adbq.desktop"
install -m 0644 "$here/share/icons/hicolor/512x512/apps/adbq.png" \
                "$prefix/share/icons/hicolor/512x512/apps/adbq.png"

# Refresh the caches, when the tools are present. Both are optimisations: the
# entry works without them, it just may take a re-login to appear.
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$prefix/share/applications" 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -qtf "$prefix/share/icons/hicolor" 2>/dev/null || true
fi

printf 'Installed adbq to %s/bin/adbq\n' "$prefix"
case ":$PATH:" in
  *":$prefix/bin:"*) ;;
  *) printf 'Note: %s/bin is not on your PATH — add it to run `adbq` from a terminal.\n' "$prefix" ;;
esac
