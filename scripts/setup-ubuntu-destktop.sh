#!/usr/bin/env bash
set -euo pipefail

SCHEMA="org.gnome.shell.extensions.dash-to-dock"

if ! command -v gsettings >/dev/null 2>&1; then
  echo "Error: gsettings not found. This script must run on a GNOME desktop."
  exit 1
fi

if ! gsettings list-schemas | grep -qx "$SCHEMA"; then
  echo "Error: GNOME Dock schema not found: $SCHEMA"
  echo "Make sure Ubuntu Dock (dash-to-dock) is installed and enabled."
  exit 1
fi

echo "Applying Ubuntu Dock settings..."

set_key_if_present() {
  local key="$1"
  local value="$2"
  if gsettings list-keys "$SCHEMA" | grep -qx "$key"; then
    gsettings set "$SCHEMA" "$key" "$value"
  else
    echo "Skipping missing key: $key"
  fi
}

# Panel mode on
set_key_if_present extend-height true

# Center icons while staying in panel mode (Ubuntu Dock key)
set_key_if_present always-center-icons true

# Show Apps icon at top/start
set_key_if_present show-apps-at-top true

# Dock at bottom (apps button appears at the start edge: left/top)
set_key_if_present dock-position 'BOTTOM'

# Fallback centering key for dash-to-dock variants
set_key_if_present dock-alignment 'CENTER'

echo "Done. Current values:"
for key in extend-height always-center-icons show-apps-at-top dock-position dock-alignment; do
  if gsettings list-keys "$SCHEMA" | grep -qx "$key"; then
    echo "$key: $(gsettings get "$SCHEMA" "$key")"
  fi
done

# Copy the .deb-installed .desktop file to the user's Desktop
SYSTEM_DESKTOP="/usr/share/applications/owlcms.desktop"
if [ ! -f "$SYSTEM_DESKTOP" ]; then
  echo "Error: $SYSTEM_DESKTOP not found. Install the owlcms-launcher .deb first."
  exit 1
fi

DESKTOP_DIR="$(xdg-user-dir DESKTOP 2>/dev/null || echo "$HOME/Desktop")"
mkdir -p "$DESKTOP_DIR"
# Remove any previous copy (may be root-owned from a prior sudo run)
rm -f "$DESKTOP_DIR/owlcms.desktop" 2>/dev/null || sudo rm -f "$DESKTOP_DIR/owlcms.desktop"
cp "$SYSTEM_DESKTOP" "$DESKTOP_DIR/"
chmod +x "$DESKTOP_DIR/owlcms.desktop"

echo "Created desktop icon: $DESKTOP_DIR/owlcms.desktop"
