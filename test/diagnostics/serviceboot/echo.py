"""Explicit ordinary-user TCP fixture, automatically started by its own unit."""
import socketserver

class Echo(socketserver.BaseRequestHandler):
    def handle(self):
        while data := self.request.recv(65536):
            self.request.sendall(data)

class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

with Server(("127.0.0.1", 19881), Echo) as server:
    server.serve_forever()
