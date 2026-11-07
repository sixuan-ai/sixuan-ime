#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "$0")/.." && pwd)
build_dir=$(mktemp -d /tmp/sixuan-desktop-schema.XXXXXX)
trap 'rm -rf "$build_dir"' EXIT
mkdir -p "$build_dir/empty"
cc -Wall -Wextra -Wno-unused-parameter -Wno-deprecated-declarations \
  -ffunction-sections -fdata-sections -Wl,--gc-sections \
  -o "$build_dir/desktop-schema-test" "$root/tests/desktop_schema_test.c" \
  $(pkg-config --cflags --libs gtk+-3.0 ibus-1.0)
# GSETTINGS_SCHEMA_DIR alone still falls back to installed system schemas.
# Isolate XDG_DATA_DIRS too, so the default schema source is genuinely absent.
GSETTINGS_SCHEMA_DIR="$build_dir/empty" XDG_DATA_DIRS="$build_dir/empty" \
  GSETTINGS_BACKEND=memory XDG_CURRENT_DESKTOP=GNOME \
  DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent \
  G_DEBUG=fatal-warnings "$build_dir/desktop-schema-test"
echo 'PASS desktop missing schemas'
