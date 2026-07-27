#!/usr/bin/env bash
# Update the OWLCMS Control Panel from its GitHub release package on Debian-based Linux.

set -euo pipefail

REPOSITORY="owlcms/owlcms-controlpanel"
INSTALL_KIND="release"
VERSION=""

usage() {
    echo "Usage: $0 [--release|--prerelease] [--version <tag>]"
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --release)
            INSTALL_KIND="release"
            shift
            ;;
        --prerelease)
            INSTALL_KIND="prerelease"
            shift
            ;;
        --version|-v)
            if [[ $# -lt 2 ]]; then
                usage >&2
                exit 1
            fi
            VERSION="$2"
            shift 2
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            usage >&2
            exit 1
            ;;
    esac
done

if [[ "$(uname -s)" != "Linux" ]]; then
    echo "Error: this updater only supports Linux." >&2
    exit 1
fi

case "$(dpkg --print-architecture)" in
    amd64)
        asset_name="Linux_Control_Panel_Installer.deb"
        ;;
    arm64)
        asset_name="Raspberry_Pi_arm64_Control_Panel_Installer.deb"
        ;;
    *)
        echo "Error: unsupported architecture: $(dpkg --print-architecture)" >&2
        exit 1
        ;;
esac

for command in curl jq; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "Installing $command..."
        sudo apt-get update -q
        sudo apt-get install -y "$command"
    fi
done

if [[ -z "$VERSION" ]]; then
    if [[ "$INSTALL_KIND" == "release" ]]; then
        VERSION="$(curl -fsSL "https://api.github.com/repos/${REPOSITORY}/releases/latest" | jq -r '.tag_name')"
    else
        VERSION="$(curl -fsSL "https://api.github.com/repos/${REPOSITORY}/releases" | jq -r '[.[] | select(.prerelease == true)][0].tag_name')"
    fi
fi

if [[ -z "$VERSION" || "$VERSION" == "null" ]]; then
    echo "Error: could not determine the requested ${INSTALL_KIND} release." >&2
    exit 1
fi

download_url="https://github.com/${REPOSITORY}/releases/download/${VERSION}/${asset_name}"
temporary_deb="$(mktemp /tmp/owlcms-controlpanel-XXXXXX.deb)"
trap 'rm -f "$temporary_deb"' EXIT

echo "Downloading owlcms Control Panel ${VERSION}..."
curl -fsSL --output "$temporary_deb" "$download_url"

echo "Updating apt package information..."
sudo apt-get update

echo "Installing owlcms Control Panel ${VERSION}..."
sudo apt-get install -y "$temporary_deb"

echo "Updated owlcms Control Panel to ${VERSION}."