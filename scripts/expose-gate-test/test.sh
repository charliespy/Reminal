#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Inside the rig's container (run.sh). The Worker from before runs first and
# lets a visitor in; then this one replaces it, on the same storage, and the
# gate is driven as browsers from several addresses would drive it.
set -u
fails=0
ok() { echo "  ok    $1"; }
bad() { echo "  FAIL  $1"; fails=$((fails + 1)); }
W=http://127.0.0.1:8787

cd /w/cloudflare
# As in tunnel-cookie-test: no routes, so the Host a request carries is the
# one the Worker sees.
python3 - <<'PY2'
import re
p = "wrangler.toml"; s = open(p).read()
s = re.sub(r"^routes = \[.*?^\]\n", "", s, flags=re.S | re.M)
open(p, "w").write(s)
PY2
worker() { # worker SRC: (re)start wrangler dev on that source, same storage
  # Its own process group: npx leaves workerd behind if only it is stopped,
  # and the old Worker would go on answering.
  if [ -n "${WPID:-}" ]; then
    pkill -TERM -g "$WPID"
    for i in $(seq 1 30); do curl -s -m 1 -o /dev/null "$W/" || break; sleep 1; done
    curl -s -m 1 -o /dev/null "$W/" && { echo "the old Worker is still up"; exit 1; }
  fi
  rm -rf src && cp -r "$1" src
  WRANGLER_SEND_METRICS=false setsid npx wrangler dev --local --persist-to /w/state --ip 127.0.0.1 --port 8787 >>/tmp/wrangler.log 2>&1 &
  WPID=$!
  for i in $(seq 1 90); do curl -s -m 2 -o /dev/null "$W/" && return; sleep 1; done
  echo "wrangler did not start"; tail -30 /tmp/wrangler.log; exit 1
}
worker /w/old-src
python3 /t/backend.py &
export HOME=/tmp/h REMINAL_RELAY=ws://127.0.0.1:8787/ws REMINAL_WEB=$W
mkdir -p $HOME
reminal expose 9000 > /tmp/expose.txt 2>&1 || { cat /tmp/expose.txt; exit 1; }
URL=$(sed -n 's/.*Public URL: *//p' /tmp/expose.txt | tr -d ' ')
PIN=$(sed -n 's/.*PIN: *//p' /tmp/expose.txt | tr -d ' ')
ID=$(echo "$URL" | sed -n 's#.*/p/\([A-Za-z0-9]*\)/.*#\1#p')
[ -n "$ID" ] && [ -n "$PIN" ] || { echo "no tunnel:"; cat /tmp/expose.txt; exit 1; }
HOST="port-$(echo "$ID" | tr 'A-Z' 'a-z').reminal.app"
c() { curl -s -m 20 -H "Host: $HOST" "$@"; }
for i in $(seq 1 30); do [ "$(c -o /dev/null -w '%{http_code}' "$W/")" = 200 ] && break; sleep 1; done
echo "tunnel $ID at $HOST"

echo "the PIN"
echo "$PIN" | grep -Eq '^[0-9ABCDEFGHJKMNPQRSTVWXYZ]{12}$' && ok "12 characters of base32: $(echo "$PIN" | cut -c1-2)..." || bad "the PIN: $PIN"

# login IP PIN: POST the gate from address IP; prints the status, keeps the cookie in /tmp/ck
login() {
  c -o /dev/null -D /tmp/h.txt -w '%{http_code}' -H "CF-Connecting-IP: $1" -X POST --data-urlencode "pin=$2" --data-urlencode "to=/" "$W/__auth"
  grep -i '^set-cookie:' /tmp/h.txt | tr -d '\r' | sed 's/^[Ss]et-[Cc]ookie: *//; s/;.*//' > /tmp/ck
}
in_with() { c -o /dev/null -w '%{http_code}' -H 'Sec-Fetch-Mode: cors' -H "Cookie: $1" "$W/echo"; }

echo "a visitor let in by the Worker from before"
got=$(login 192.0.2.10 "$PIN"); OLDCK=$(cat /tmp/ck)
[ "$got" = 302 ] && [ -n "$OLDCK" ] && ok "the old gate let it in" || { bad "the old gate: $got"; tail -15 /tmp/wrangler.log; }
worker /w/new-src
for i in $(seq 1 60); do got=$(in_with "$OLDCK"); [ "$got" = 503 ] || break; sleep 1; done
[ "$got" = 200 ] && ok "after the upgrade its cookie still lets it in" || bad "after the upgrade: $got"

echo "the visitor cookie"
got=$(login 192.0.2.11 "$(echo "$PIN" | tr 'A-Z' 'a-z' | sed 's/\(....\)\(....\)/\1-\2 /')"); CK1=$(cat /tmp/ck)
[ "$got" = 302 ] && ok "the PIN typed in lower case, with a dash and a space, is taken" || bad "a PIN typed loosely: $got"
echo "$CK1" | grep -Eq '=v1\.[0-9a-f]{32}\.[0-9]+\.[0-9a-f]{64}$' && ok "a cookie of its own: v1.<visitor>.<issued>.<mac>" || bad "the cookie: $CK1"
grep -qi 'max-age=2592000' /tmp/h.txt && ok "Max-Age is 30 days" || bad "Max-Age: $(grep -i set-cookie /tmp/h.txt)"
login 192.0.2.11 "$PIN" >/dev/null; CK2=$(cat /tmp/ck)
[ "$CK1" != "$CK2" ] && ok "a second visitor gets another" || bad "two visitors got the same cookie"
g1=$(in_with "$CK1"); g2=$(in_with "$CK2")
[ "$g1" = 200 ] && [ "$g2" = 200 ] && ok "each lets its visitor in" || bad "a visitor cookie did not let it in: $g1 $g2"
TAMPER=$(echo "$CK1" | sed 's/\.\([0-9]*\)\./.1\1./')
[ "$(in_with "$TAMPER")" = 401 ] && ok "one with its issue time changed does not" || bad "a changed cookie let it in"

echo "wrong PINs from one address"
A=203.0.113.5
codes=""; for i in 1 2 3 4 5; do codes="$codes$(login $A WRONGPIN0000) "; done
[ "$codes" = "401 401 401 401 401 " ] && ok "five wrong PINs: $codes" || bad "five wrong PINs: $codes"
got=$(login $A "$PIN"); ra=$(grep -i '^retry-after:' /tmp/h.txt | tr -d '\r' | sed 's/.*: *//')
[ "$got" = 429 ] && [ "${ra:-0}" -gt 290 ] && [ "${ra:-0}" -le 300 ] && ok "then even the right one waits: $got, Retry-After $ra" || bad "after five wrong: $got, Retry-After ${ra:-none}"
c -H "CF-Connecting-IP: $A" -X POST --data-urlencode "pin=$PIN" "$W/__auth" | grep -q "Try again in 5 minutes" && ok "the page says how long" || bad "the page does not say how long"
got=$(login 198.51.100.7 "$PIN")
[ "$got" = 302 ] && ok "another address is not held by it: $got" || bad "another address: $got"

echo "wrong PINs from many addresses"
codes=""; for n in $(seq 20 29); do for i in 1 2 3 4 5; do codes="$codes$(login 203.0.113.$n WRONGPIN0000) "; done; done
echo "$codes" | grep -q 401 && ok "fifty more wrong PINs, from ten addresses" || bad "many addresses: $codes"
got=$(login 203.0.113.99 "$PIN")
[ "$got" = 302 ] && ok "with a long PIN, a new address is not held by them: $got" || bad "a new address, long PIN: $got"
[ "$(in_with "$CK1")" = 200 ] && ok "and a visitor holding a cookie is not asked at all" || bad "a visitor with a cookie was refused"
reminal stop 9000 >/dev/null 2>&1

echo "a forward with a six-digit PIN from before (carried across a restart)"
for i in $(seq 1 30); do [ "$(c -o /dev/null -w '%{http_code}' "$W/")" = 200 ] || break; sleep 1; done
SHORT=123456 SID=GATE6DGT
HASH=$(node -e "process.stdout.write(require('/w/cloudflare/node_modules/bcryptjs').hashSync('$SHORT', 10))")
REMINAL_EXPOSE_RESUME=1 REMINAL_EXPOSE_SESSION_ID=$SID REMINAL_EXPOSE_PIN=$SHORT REMINAL_EXPOSE_PIN_HASH=$HASH \
  reminal expose 9000 > /tmp/expose6.txt 2>&1 || { cat /tmp/expose6.txt; exit 1; }
HOST="port-$(echo "$SID" | tr 'A-Z' 'a-z').reminal.app"
for i in $(seq 1 30); do [ "$(c -o /dev/null -w '%{http_code}' "$W/")" = 200 ] && break; sleep 1; done
got=$(login 198.51.100.7 "$SHORT")
[ "$got" = 302 ] && ok "its PIN still opens it: $got" || { bad "the six-digit PIN: $got"; cat /tmp/expose6.txt; }
for n in $(seq 40 49); do for i in 1 2 3 4 5; do login 203.0.113.$n 000000 >/dev/null; done; done
got=$(login 203.0.113.98 "$SHORT")
[ "$got" = 429 ] && ok "fifty wrong PINs from ten addresses make a new address wait: $got" || bad "a new address, short PIN: $got"
got=$(login 198.51.100.7 "$SHORT")
[ "$got" = 302 ] && ok "but not one it has let in before: $got" || bad "an address let in before, short PIN: $got"
reminal stop 9000 >/dev/null 2>&1
echo
[ "$fails" = 0 ] && echo "PASS" || { echo "FAIL ($fails)"; tail -20 /tmp/wrangler.log; exit 1; }
