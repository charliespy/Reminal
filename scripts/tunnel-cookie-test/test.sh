#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Inside the rig's container (run.sh): the Worker under wrangler dev, the app
# (backend.py), and `reminal expose` to it; then requests as a browser would
# send them to port-<id>.reminal.app, by its Host header.
set -u
fails=0
ok() { echo "  ok    $1"; }
bad() { echo "  FAIL  $1"; fails=$((fails + 1)); }
W=http://127.0.0.1:8787

cd /w/cloudflare
# wrangler dev hands the Worker the Host of the routes in wrangler.toml, not
# the client's. Without them the Host a request carries is the one the Worker
# sees, as on the hosted relay. (Only this rig's copy of the file changes.)
python3 - <<'PY2'
import re
p = "wrangler.toml"; s = open(p).read()
s = re.sub(r"^routes = \[.*?^\]\n", "", s, flags=re.S | re.M)
open(p, "w").write(s)
PY2
grep -q '^routes' wrangler.toml && { echo "wrangler.toml still has routes"; exit 1; }
WRANGLER_SEND_METRICS=false npx wrangler dev --local --ip 127.0.0.1 --port 8787 >/tmp/wrangler.log 2>&1 &
for i in $(seq 1 90); do curl -s -m 2 -o /dev/null "$W/" && break; sleep 1; done
python3 /t/backend.py &
export HOME=/tmp/h REMINAL_RELAY=ws://127.0.0.1:8787/ws REMINAL_WEB=$W
mkdir -p $HOME
reminal expose 9000 > /tmp/expose.txt 2>&1 || { cat /tmp/expose.txt; exit 1; }
URL=$(sed -n 's/.*Public URL: *//p' /tmp/expose.txt | tr -d ' ')
PIN=$(sed -n 's/.*PIN: *//p' /tmp/expose.txt | tr -d ' ')
ID=$(echo "$URL" | sed -n 's#.*/p/\([A-Za-z0-9]*\)/.*#\1#p')
[ -n "$ID" ] && [ -n "$PIN" ] || { echo "no tunnel:"; cat /tmp/expose.txt; tail -20 /tmp/wrangler.log; exit 1; }
HOST="port-$(echo "$ID" | tr 'A-Z' 'a-z').reminal.app"
for i in $(seq 1 30); do [ "$(curl -s -m 2 -o /dev/null -w '%{http_code}' -H "Host: $HOST" "$W/")" = 200 ] && break; sleep 1; done
echo "tunnel $ID at $HOST"

c() { curl -s -m 10 -H "Host: $HOST" "$@"; }

echo "the gate's cookie"
c -o /dev/null -D /tmp/auth.h -X POST --data-urlencode "pin=$PIN" --data-urlencode "to=/" "$W/__auth"
GATE=$(grep -i '^set-cookie:' /tmp/auth.h | tr -d '\r' | sed 's/^[Ss]et-[Cc]ookie: *//')
case "$GATE" in
  "__Host-reminal_auth_$ID="*) ok "named __Host-reminal_auth_<id>" ;;
  *) bad "the gate's cookie: $GATE" ;;
esac
echo "$GATE" | grep -q 'Path=/;' && ! echo "$GATE" | grep -qi 'domain=' && echo "$GATE" | grep -q 'Secure' \
  && ok "Path=/, Secure, no Domain (what __Host- requires)" || bad "attributes: $GATE"
# From here on, the cookie the gate did set, whatever its name: each check
# below then fails for its own reason only.
CK=$(echo "$GATE" | sed 's/;.*//')
GNAME=${CK%%=*}
# The tunnel reconnects to the restarted Worker in its own time (backoff).
for i in $(seq 1 60); do c -o /dev/null -w '%{http_code}' -H "Cookie: $CK" "$W/echo" | grep -q 503 || break; sleep 1; done

echo "an app's Set-Cookie"
c -o /dev/null -D /tmp/set.h -H "Cookie: $CK" "$W/set?id=$ID"
SETS=$(grep -i '^set-cookie:' /tmp/set.h | tr -d '\r')
echo "$SETS" | sed 's/^/        /'
echo "$SETS" | grep -qi 'domain' && bad "a Domain reaches the visitor" || ok "no Domain reaches the visitor"
for n in tossed dotted loop plain; do
  echo "$SETS" | grep -q "$n=1" && ok "$n is kept (host-only)" || bad "$n was dropped"
done
echo "$SETS" | grep -qi 'reminal_auth' && bad "the app set reminal's gate cookie" || ok "no app can set the gate's cookie"

echo "requests from another tunnel (Sec-Fetch-Site: same-site)"
code() { c -o /dev/null -w '%{http_code}' -H "Cookie: $CK" "$@"; }
want() { got=$(code "$@"); [ "$got" = "$w" ] && ok "$what: $got" || bad "$what: $got, want $w"; }
w=403 what="fetch POST" want -X POST -H 'Sec-Fetch-Site: same-site' -H 'Sec-Fetch-Mode: cors' -d x=1 "$W/echo"
w=403 what="form POST (a navigation)" want -X POST -H 'Sec-Fetch-Site: same-site' -H 'Sec-Fetch-Mode: navigate' -d x=1 "$W/echo"
w=403 what="fetch GET" want -H 'Sec-Fetch-Site: same-site' -H 'Sec-Fetch-Mode: cors' "$W/echo"
w=403 what="an <img> GET" want -H 'Sec-Fetch-Site: same-site' -H 'Sec-Fetch-Mode: no-cors' "$W/echo"
w=200 what="a link followed (GET navigation)" want -H 'Sec-Fetch-Site: same-site' -H 'Sec-Fetch-Mode: navigate' "$W/echo"
w=200 what="the tunnel's own fetch POST (same-origin)" want -X POST -H 'Sec-Fetch-Site: same-origin' -H 'Sec-Fetch-Mode: cors' -d x=1 "$W/echo"
w=200 what="from another site's link (cross-site GET navigation)" want -H 'Sec-Fetch-Site: cross-site' -H 'Sec-Fetch-Mode: navigate' "$W/echo"
w=200 what="no Sec-Fetch headers (curl, older browsers)" want -X POST -d x=1 "$W/echo"
WS="-H Connection:Upgrade -H Upgrade:websocket -H Sec-WebSocket-Version:13 -H Sec-WebSocket-Key:dGhlIHNhbXBsZSBub25jZQ=="
w=403 what="a WebSocket" want $WS -H 'Sec-Fetch-Site: same-site' -H 'Sec-Fetch-Mode: websocket' --max-time 5 "$W/ws"
got=$(curl -s -o /dev/null -w '%{http_code}' -H "Host: $HOST" $WS --max-time 5 "$W/ws"); [ "$got" = 401 ] && ok "a WebSocket with no gate cookie: 401" || bad "a WebSocket with no gate cookie: $got"

echo "a cookie of the same name set elsewhere on the site"
body=$(c -H "Cookie: $CK; $GNAME=shadow" "$W/echo")
case "$body" in METHOD*) ok "the visitor's own (first) wins" ;; *) bad "shadowed: $(echo "$body" | head -c 80)" ;; esac

echo "what reaches the app"
body=$(c -H "Cookie: $CK; reminal_auth_$ID=old; __Host-reminal_auth_$ID=x; app=1" "$W/echo")
[ "$body" = "METHOD GET COOKIE [app=1]" ] && ok "only the app's own cookies: $body" || bad "the app got: $body"

reminal stop 9000 >/dev/null 2>&1
echo
[ "$fails" = 0 ] && echo "PASS" || { echo "FAIL ($fails)"; exit 1; }
