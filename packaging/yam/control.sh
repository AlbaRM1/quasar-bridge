#!/bin/sh
# Run on the speaker, over SSH. Installing the package alone never changes firmware.
set -eu
package=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
state=${QUASAR_DATA_DIR:-/data/opt/quasar-bridge}
action=${1:?usage: control.sh check|restart|apply|rollback}
case "$action" in
 restart)
  yam=${QUASAR_YAM_URL:-http://127.0.0.1:8099}
  health=http://127.0.0.1:8443/healthz
  wget -q -O /dev/null --post-data='' "$yam/api/plugins/quasar-bridge/stop"
  i=0
  while wget -q -O /dev/null "$health"; do
   i=$((i+1)); test "$i" -lt 15 || { echo 'old listener is still active' >&2; exit 1; }
   sleep 1
  done
  wget -q -O /dev/null --post-data='' "$yam/api/plugins/quasar-bridge/service/start"
  i=0
  until wget -q -O /dev/null "$health"; do
   i=$((i+1)); test "$i" -lt 15 || { echo 'new listener did not become ready; inspect yam logs' >&2; exit 1; }
   sleep 1
  done
  echo 'Bridge restarted and healthy'
  exit 0
  ;;
 check) exec sh "$package/start.sh" -check-config ;;
 apply)
  # Require a running bridge before changing the speaker's endpoint.
  wget -q -O /dev/null http://127.0.0.1:8443/healthz
  ;;
 rollback) test -s "$state/endpoint-backup.json" ;;
 *) echo 'usage: control.sh check|restart|apply|rollback' >&2; exit 2 ;;
esac
cfg=${QUASAR_DEVICE_CONFIG:-/system/vendor/quasar/quasar.cfg}
# Firmware root is read-only on supported rooted Stations. Always restore that state.
mount -o remount,rw /
trap 'sync; mount -o remount,ro /' EXIT HUP INT TERM
"$package/bin/quasar-bridge" -speaker-endpoint "$action" \
 -speaker-config "$cfg" -speaker-backup "$state/endpoint-backup.json"
sync
mount -o remount,ro /
trap - EXIT HUP INT TERM
printf '%s\n' 'Endpoint saved. Restart /etc/init.d/S99quasar (or reboot) when ready.'
