#!/bin/sh
set -eu
backup=${QUASAR_DEVICE_BACKUP_DIR:-/data/opt/quasar-bridge-backup}
cfg=${QUASAR_DEVICE_CONFIG:-/system/vendor/quasar/quasar.cfg}
ca=${QUASAR_DEVICE_CA_BUNDLE:-/system/vendor/quasar/ca-certificates.crt}
test -s "$backup/quasar.cfg.orig"
test -s "$backup/ca-certificates.crt.orig"
mount -o remount,rw /
trap 'sync; mount -o remount,ro /' EXIT
cat "$backup/quasar.cfg.orig" > "$cfg"
cat "$backup/ca-certificates.crt.orig" > "$ca"
rm -f "$backup/applied"
sync
mount -o remount,ro /
trap - EXIT
echo 'Original endpoints and CA bundle restored; reboot to apply'
