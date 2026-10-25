#!/bin/sh
# Run on the rooted pilot speaker after staging cfg and the bridge's PUBLIC CA.
set -eu
stage=${1:?staging directory required}
backup=${QUASAR_DEVICE_BACKUP_DIR:-/data/opt/quasar-bridge-backup}
cfg=${QUASAR_DEVICE_CONFIG:-/system/vendor/quasar/quasar.cfg}
ca=${QUASAR_DEVICE_CA_BUNDLE:-/system/vendor/quasar/ca-certificates.crt}
test -s "$stage/quasar.cfg.pilot"
test -s "$stage/bridge-ca.pem"
test ! -e "$backup/applied" || { echo 'pilot already applied' >&2; exit 1; }
mkdir -p "$backup"
test -e "$backup/quasar.cfg.orig" || cp -p "$cfg" "$backup/quasar.cfg.orig"
test -e "$backup/ca-certificates.crt.orig" || cp -p "$ca" "$backup/ca-certificates.crt.orig"
mount -o remount,rw /
trap 'sync; mount -o remount,ro /' EXIT
cat "$stage/quasar.cfg.pilot" > "$cfg"
printf '\n' >> "$ca"
cat "$stage/bridge-ca.pem" >> "$ca"
touch "$backup/applied"
sync
mount -o remount,ro /
trap - EXIT
echo 'Pilot installed; backups saved under /data/opt/quasar-bridge-backup'
