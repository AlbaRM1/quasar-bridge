#!/bin/sh
set -eu
umask 077
package=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
state=${QUASAR_DATA_DIR:-/data/opt/quasar-bridge}
mkdir -p "$state"
chmod 0700 "$state"
if [ ! -f "$state/config.json" ]; then
 cp "$package/config.device.example.json" "$state/config.json"
 chmod 0600 "$state/config.json"
fi
if [ -f "$state/runtime.env" ]; then
 set -a
 . "$state/runtime.env"
 set +a
fi
export SSL_CERT_FILE=${SSL_CERT_FILE:-/system/vendor/quasar/ca-certificates.crt}
export GOMAXPROCS=${GOMAXPROCS:-1}
export GOMEMLIMIT=${GOMEMLIMIT:-64MiB}
cd "$state"
# exec is essential: yamd stops the child process, not a shell's descendants.
exec "$package/bin/quasar-bridge" -config "$state/config.json" "$@"
