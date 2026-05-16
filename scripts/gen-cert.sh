#!/usr/bin/env bash
# Generate a self-signed certificate ATV3 will trust for appletv.redbull.tv.
# Output: certs/redbulltv.pem (cert+key), redbulltv.key, redbulltv.cer (DER).
set -euo pipefail

OUT="${1:-certs}"
HOST="${2:-appletv.redbull.tv}"

mkdir -p "$OUT"

if [[ -f "$OUT/redbulltv.pem" && -f "$OUT/redbulltv.key" && -f "$OUT/redbulltv.cer" ]]; then
  echo "Certificate already exists in $OUT — skipping. Remove files to regenerate."
  exit 0
fi

openssl req -new -nodes -newkey rsa:2048 \
  -out "$OUT/redbulltv.pem" -keyout "$OUT/redbulltv.key" \
  -x509 -days 7300 \
  -subj "/C=US/CN=$HOST" \
  -addext "subjectAltName=DNS:$HOST"

openssl x509 -in "$OUT/redbulltv.pem" -outform der -out "$OUT/redbulltv.cer"

# Server expects key concatenated into the .pem for TLS loading.
cat "$OUT/redbulltv.key" >> "$OUT/redbulltv.pem"

echo "Generated certificate for $HOST in $OUT/"
