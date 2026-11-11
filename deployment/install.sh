#!/bin/sh
# Verify this script's published SHA-256 before running it. No curl|sh path.
set -eu
umask 077
CONFIG_BASE64=
AMD64_URL=
AMD64_SHA=
ARM64_URL=
ARM64_SHA=
SYSTEM_USER=
CONFIG=
CHECK=
while [ "$#" -gt 0 ]; do
 case "$1" in
  --config-base64) CONFIG_BASE64=$2; shift 2 ;;
  --config) CONFIG=$2; shift 2 ;;
  --amd64-url) AMD64_URL=$2; shift 2 ;;
  --amd64-sha256) AMD64_SHA=$2; shift 2 ;;
  --arm64-url) ARM64_URL=$2; shift 2 ;;
  --arm64-sha256) ARM64_SHA=$2; shift 2 ;;
  --system-user) SYSTEM_USER=$2; shift 2 ;;
  --check) CHECK=1; shift ;;
  *) printf '%s\n' 'Invalid installer option.' >&2; exit 2 ;;
 esac
done
[ "$(uname -s)" = Linux ] || { printf '%s\n' 'Linux is required.' >&2; exit 2; }
for command in curl sha256sum mktemp base64 uname chmod; do
 command -v "$command" >/dev/null 2>&1 || { printf 'Missing prerequisite: %s\n' "$command" >&2; exit 2; }
done
case "$(uname -m)" in
 x86_64|amd64) URL=$AMD64_URL; SHA=$AMD64_SHA ;;
 aarch64|arm64) URL=$ARM64_URL; SHA=$ARM64_SHA ;;
 *) printf '%s\n' 'Only Linux amd64/arm64 is supported.' >&2; exit 2 ;;
esac
case "$URL" in https://*) ;; *) printf '%s\n' 'A trusted HTTPS bootstrap URL is required.' >&2; exit 2 ;; esac
[ "${#SHA}" -eq 64 ] || exit 2
case "$SHA" in *[!0-9a-f]*) exit 2 ;; esac
TEMP=$(mktemp -d)
trap 'rm -rf -- "$TEMP"' EXIT HUP INT TERM
chmod 700 "$TEMP"
if [ -n "$CONFIG_BASE64" ]; then
 [ -z "$CONFIG" ] || exit 2
 printf '%s' "$CONFIG_BASE64" | base64 --decode > "$TEMP/install.json"
 chmod 600 "$TEMP/install.json"
 CONFIG=$TEMP/install.json
fi
[ -n "$CONFIG" ] || { printf '%s\n' 'A private installation config is required.' >&2; exit 2; }
# No redirect, environment proxy, retry or insecure TLS fallback.
curl --fail --silent --show-error --proto '=https' --tlsv1.3 --noproxy '*' --max-time 180 --max-filesize 268435456 "$URL" --output "$TEMP/controller"
printf '%s  %s\n' "$SHA" "$TEMP/controller" | sha256sum --check --status
chmod 700 "$TEMP/controller"
set -- install -config "$CONFIG"
[ -z "$SYSTEM_USER" ] || set -- "$@" -system-user "$SYSTEM_USER"
[ -z "$CHECK" ] || set -- "$@" -check
"$TEMP/controller" "$@"
