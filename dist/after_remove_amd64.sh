#!/bin/bash

if [[ -n "$SUDO_USER" ]]; then
  target_user="$SUDO_USER"
elif [[ -n "$PACKAGEKIT_CALLER_UID" ]]; then
  target_user=$(getent passwd "$PACKAGEKIT_CALLER_UID" | cut -d: -f1)
else
  echo "No user found to remove the desktop file" > /tmp/owlcms.log
  printenv >> /tmp/owlcms.log
  exit 0
fi

user_home=$(getent passwd "$target_user" | cut -d: -f6)
desktop_dir=$(sudo -u "$target_user" xdg-user-dir DESKTOP 2>/dev/null)
if [[ -z "$desktop_dir" ]]; then
  desktop_dir="$user_home/Desktop"
fi

rm -f "$desktop_dir/owlcms.desktop"