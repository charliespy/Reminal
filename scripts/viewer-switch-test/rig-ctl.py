# Test-rig control in the agents' container: GET /restart/<id> runs `reminal restart <id>`.
import http.server, subprocess, sys
B = sys.argv[1]
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        parts = self.path.strip('/').split('/')
        if len(parts) == 2 and parts[0] == 'restart' and parts[1].isalnum():
            r = subprocess.run([B, 'restart', parts[1]], cwd='/root/h', capture_output=True, text=True, timeout=30)
            body = (r.stdout + r.stderr).encode()
            self.send_response(200); self.end_headers(); self.wfile.write(body)
        elif parts == ['relay-restart']:
            subprocess.run(['sh', '-c', "for p in $(pgrep -f '^/p/bin/[r]em[^ ]* relay 8080'); do kill $p; done; sleep 0.3; cd /root/h; (setsid " + B + " relay 8080 >/root/relay.log 2>&1 &)"], timeout=10)
            self.send_response(200); self.end_headers(); self.wfile.write(b'ok')
        else:
            self.send_response(404); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', 18999), H).serve_forever()
