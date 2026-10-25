#!/bin/sh
set -eu
umask 077
dir=${1:?certificate output directory required}
san=${2:?certificate SAN required (DNS:hostname or IP:address)}
printf '%s\n' "$san" | LC_ALL=C grep -Eq '^(IP:[[:xdigit:].:]+|DNS:[[:alnum:].-]+)$' || {
  echo 'invalid SAN; use DNS:hostname or IP:address' >&2; exit 1;
}
mkdir -p "$dir"
for name in ca.key ca.pem server.key server.pem; do
  test ! -e "$dir/$name" || { echo "refusing to replace existing $name" >&2; exit 1; }
done
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 365 \
  -subj '/CN=Local Quasar Bridge CA' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$dir/ca.key" -out "$dir/ca.pem" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -sha256 -subj '/CN=Local Quasar Bridge' \
  -keyout "$dir/server.key" -out "$dir/server.csr" 2>/dev/null
cat > "$dir/server.ext" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=$san
EOF
openssl x509 -req -in "$dir/server.csr" -CA "$dir/ca.pem" -CAkey "$dir/ca.key" \
  -CAcreateserial -days 90 -sha256 -extfile "$dir/server.ext" -out "$dir/server.pem" 2>/dev/null
openssl verify -CAfile "$dir/ca.pem" "$dir/server.pem"
