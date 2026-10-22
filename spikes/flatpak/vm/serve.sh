#!/bin/sh
# Serves the spike's repositories and links at http://localhost:8080/ from
# the shared folder, as the release host will serve Dens's, with the media
# types software centers expect. Leave it running; Ctrl+C stops it.
cd "$(dirname "$0")/../web" || exit 1
echo "Serving $(pwd) at http://localhost:8080/"
exec python3 -c '
import http.server
class Handler(http.server.SimpleHTTPRequestHandler):
    extensions_map = {**http.server.SimpleHTTPRequestHandler.extensions_map,
        ".flatpakref": "application/vnd.flatpak.ref",
        ".flatpakrepo": "application/vnd.flatpak.repo"}
http.server.ThreadingHTTPServer(("127.0.0.1", 8080), Handler).serve_forever()
'
