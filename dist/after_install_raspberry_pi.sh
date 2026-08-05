#!/bin/bash

# Let owlcms listen on port 80 without root, as it already can on macOS and Windows.
# Done before the desktop-user detection below, which exits early on headless installs.
echo 'net.ipv4.ip_unprivileged_port_start=80' > /etc/sysctl.d/50-owlcms.conf
sysctl --system >/dev/null 2>&1 || true

if [[ -n "$SUDO_USER" ]]; then
  target_user="$SUDO_USER"
elif [[ -n "$PACKAGEKIT_CALLER_UID" ]]; then
  target_user=$(getent passwd "$PACKAGEKIT_CALLER_UID" | cut -d: -f1)
else
  echo "No user found to create the desktop file" > /tmp/owlcms.log
  printenv >> /tmp/owlcms.log
  exit 0
fi

user_home=$(getent passwd "$target_user" | cut -d: -f6)
desktop_dir=$(sudo -u "$target_user" xdg-user-dir DESKTOP 2>/dev/null)
if [[ -z "$desktop_dir" ]]; then
  desktop_dir="$user_home/Desktop"
fi

mkdir -p "$desktop_dir"
shortcut_path="$desktop_dir/owlcms.desktop"

cat > "$shortcut_path" <<EOF
[Desktop Entry]
Type=Application
Name=owlcms
Exec=controlpanel
Icon=owlcms
Terminal=false
EOF

chmod +x "$shortcut_path"