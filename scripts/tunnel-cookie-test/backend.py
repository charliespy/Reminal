# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# The app behind the tunnel: /set answers with the cookies a hostile (or
# merely careless) app might set, /echo with what reached it.
import http.server, urllib.parse

class H(http.server.BaseHTTPRequestHandler):
    def answer(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query)
        self.send_response(200)
        if u.path == "/set":
            sid = q.get("id", [""])[0]
            self.send_header("Set-Cookie", "tossed=1; Domain=reminal.app; Path=/")
            self.send_header("Set-Cookie", "dotted=1; Path=/; domain=.reminal.app; HttpOnly")
            self.send_header("Set-Cookie", "loop=1; Domain=127.0.0.1; Path=/")
            self.send_header("Set-Cookie", "plain=1; Path=/")
            self.send_header("Set-Cookie", "__Host-reminal_auth_%s=fromapp; Path=/; Secure" % sid)
            self.send_header("Set-Cookie", "__HOST-REMINAL_AUTH_%s=fromapp; Path=/; Secure" % sid)
            self.send_header("Set-Cookie", "reminal_auth_%s=fromapp; Path=/" % sid)
        self.send_header("Content-Type", "text/plain")
        self.end_headers()
        self.wfile.write(("METHOD %s COOKIE [%s]\n" % (self.command, self.headers.get("Cookie", ""))).encode())
    def do_GET(self): self.answer()
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", "0") or 0))
        self.answer()
    def log_message(self, *a): pass

http.server.ThreadingHTTPServer(("127.0.0.1", 9000), H).serve_forever()
