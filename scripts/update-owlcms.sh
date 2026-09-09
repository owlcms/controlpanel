#!/usr/bin/env bash
# Update and restart the default OWLCMS instance.

set -euo pipefail

target_version="${1:-latest}"

if [[ $# -gt 1 ]]; then
    echo "Usage: $0 [target-version]" >&2
    exit 1
fi

printf '%s\n' \
    'owlcms stop' \
    "owlcms update A $target_version" \
    'owlcms start' |
    controlpanel --instance owlcms --batch -