#!/usr/bin/env bash
# Update and restart the default OWLCMS instance.

set -euo pipefail

target_version="${1:-latest}"

if [[ $# -gt 1 ]]; then
    echo "Usage: $0 [target-version]" >&2
    exit 1
fi

controlpanel --instance owlcms --module owlcms --stop
controlpanel --instance owlcms --module owlcms --version latest --update-to "$target_version"
controlpanel --instance owlcms --module owlcms --version latest --launch --background