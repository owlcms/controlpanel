#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_app="/Applications/owlcms.app"
output_app="$repo_root/build/macos-local/owlcms.app"
executable=""
identity="-"

usage() {
  printf '%s\n' \
    'Usage: bash scripts/package-local-macos.sh [options]' \
    '  --source-app PATH  Existing bundle (default: /Applications/owlcms.app)' \
    '  --output PATH      New .app path (default: build/macos-local/owlcms.app)' \
    '  --executable PATH  Replace the bundled executable with an existing binary' \
    '  --identity NAME    Signing identity (default: - for local ad-hoc signing)' \
    'No build, download, notarization, installation, or permission reset is performed.'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --source-app|--output|--executable|--identity)
      if [[ $# -lt 2 || -z "$2" ]]; then
        printf 'Missing value for %s\n' "$1" >&2
        exit 1
      fi
      case "$1" in
        --source-app) source_app="$2" ;;
        --output) output_app="$2" ;;
        --executable) executable="$2" ;;
        --identity) identity="$2" ;;
      esac
      shift 2
      ;;
    *) printf 'Unknown option: %s\n' "$1" >&2; usage >&2; exit 1 ;;
  esac
done

if [[ "$(uname -s)" != Darwin ]]; then
  printf 'This script requires macOS.\n' >&2
  exit 1
fi
if [[ ! -f "$source_app/Contents/Info.plist" ]]; then
  printf 'Source app is missing Contents/Info.plist: %s\n' "$source_app" >&2
  exit 1
fi
if [[ "$output_app" != *.app || -e "$output_app" || -L "$output_app" ]]; then
  printf 'Output must be a new .app path; existing paths are never overwritten: %s\n' "$output_app" >&2
  exit 1
fi
if [[ -n "$executable" && ! -f "$executable" ]]; then
  printf 'Executable not found: %s\n' "$executable" >&2
  exit 1
fi

bundle_executable=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$source_app/Contents/Info.plist")
if [[ -z "$bundle_executable" || "$bundle_executable" == */* || "$bundle_executable" == .* ]]; then
  printf 'Invalid CFBundleExecutable: %s\n' "$bundle_executable" >&2
  exit 1
fi

mkdir -p "$(dirname "$output_app")"
ditto "$source_app" "$output_app"
if [[ -n "$executable" ]]; then
  cp "$executable" "$output_app/Contents/MacOS/$bundle_executable"
  chmod 755 "$output_app/Contents/MacOS/$bundle_executable"
fi

info_plist="$output_app/Contents/Info.plist"
plutil -replace NSCameraUsageDescription \
  -string 'OWLCMS uses connected cameras for competition video and replay.' \
  "$info_plist"
codesign --force --deep --options runtime --timestamp=none \
  --entitlements "$repo_root/dist/owlcms.entitlements" \
  --sign "$identity" "$output_app"
codesign --verify --deep --strict "$output_app"
plutil -lint "$info_plist"
plutil -extract NSCameraUsageDescription raw -o - "$info_plist"
printf '\n'
codesign -d --entitlements - "$output_app"

printf '\nCreated %s\n' "$output_app"
printf 'Quit the running control panel, then open this exact bundle in Finder.\n'
printf 'Start Video from it and allow camera access when prompted.\n'
printf 'Do not launch Contents/MacOS/%s directly from Terminal for this test.\n' "$bundle_executable"
if [[ "$identity" == - ]]; then
  printf 'Ad-hoc signing is for local testing; use --identity with your Developer ID for release-like identity testing.\n'
fi