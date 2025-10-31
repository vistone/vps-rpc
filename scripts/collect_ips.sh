#!/usr/bin/env bash
set -euo pipefail
DOMAIN=${1:-kh.google.com}
PATH_URI=${2:-/rt/earth/PlanetoidMetadata}
LOOPS=${3:-20}
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
for i in $(seq 1 "$LOOPS"); do
  curl -k -s -o /dev/null -v "https://$DOMAIN$PATH_URI" 2>&1 |
    awk '/\* Trying /{print $3}/Connected to /{gsub(/[()]/," "); print $4}' >> "$TMP" || true
  sleep 0.2
done
# normalize ipv6 brackets removal if any
sed -i 's/\[//g;s/\]//g' "$TMP"
sort -u "$TMP" | grep -E '^[0-9a-fA-F:.]+$' || true
