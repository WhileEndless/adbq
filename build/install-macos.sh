#!/bin/sh
# Stage and verify the replacement on the destination volume before moving the old app.
set -eu

source_bundle=$1
install_dir=${2:?missing installation directory}
destination="$install_dir/adbq.app"
stage="$install_dir/.adbq-install"
incoming="$stage/adbq.app"
backup="$stage/previous.app"

if [ ! -x "$source_bundle/Contents/MacOS/adbq" ]; then
    printf 'Missing built app at %s\n' "$source_bundle" >&2
    exit 1
fi

bundle_id() {
    /usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$1/Contents/Info.plist"
}

mkdir -p "$install_dir"
if ! mkdir "$stage"; then
    printf 'Could not reserve %s for installation. Check permissions and concurrent installs.\n' "$stage" >&2
    exit 1
fi

cleanup() {
    status=$?
    trap - EXIT HUP INT TERM
    if [ -d "$backup" ] && [ ! -e "$destination" ] && [ ! -L "$destination" ]; then
        if ! mv "$backup" "$destination"; then
            printf 'Could not restore the previous app. Its bundle is at %s\n' "$backup" >&2
            exit 1
        fi
    fi
    rm -rf "$stage"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

if [ -L "$destination" ] || { [ -e "$destination" ] && [ ! -d "$destination" ]; }; then
    printf 'Expected an application bundle at %s\n' "$destination" >&2
    exit 1
fi

/usr/bin/ditto "$source_bundle" "$incoming"
/usr/bin/codesign --verify --deep --strict "$incoming"
if [ -d "$destination" ]; then
    incoming_id=$(bundle_id "$incoming")
    if ! installed_id=$(bundle_id "$destination" 2>/dev/null) || [ "$incoming_id" != "$installed_id" ]; then
        printf 'The bundle at %s has a different identity. Choose another INSTALL_DIR.\n' "$destination" >&2
        exit 1
    fi
    mv "$destination" "$backup"
fi
mv "$incoming" "$destination"
printf 'Installed %s\n' "$destination"
