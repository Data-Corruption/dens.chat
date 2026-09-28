# M1 spikes

Throwaway experiments answering M1 questions before the code that depends on them. This is its own Go module, so its dependencies never reach the product. It is deleted when M1 lands; the findings live in `docs/dev/design.md` and `docs/dev/protocol.md`.

## densocket: den WebSockets through Caddy

`densocket/run.sh` builds a den-shaped server and client on `github.com/coder/websocket`, and:

1. Measures, on loopback, what permessage-deflate saves on 500 chat-shaped events and what it costs the den in heap per connection.
2. Compares, offline, per-frame deflate with batching, shorter JSON keys and a static preset dictionary.
3. Runs the server behind Caddy (`tls internal`) in an Incus container. From the host, it checks authentication and version errors through the proxy, then follows the event stream for 40 seconds through a Caddy reload, a Caddy restart and a den restart, counting lost events.

```sh
spikes/densocket/run.sh    # results in out/spikes/densocket/<run>/
```

### Results (Caddy 2.6.2 on Debian 13, coder/websocket v1.8.15)

Compression of 500 events (about 200 bytes each):

| Mode | Wire bytes vs raw | Den heap per connection |
| --- | --- | --- |
| None | 100% | 25 KB |
| permessage-deflate, library default (skips messages under 512 B) | 98% | |
| permessage-deflate on every message, no context takeover | 78% | 23 KB |
| permessage-deflate with context takeover | 35% | 821 KB |

Per-frame deflate without context takeover, offline:

| Encoding | Bytes vs raw |
| --- | --- |
| One event per frame | 75% |
| 5 events per frame | 40% |
| 20 events per frame | 29% |
| Short keys, raw | 82% |
| Short keys, one event per frame | 67% |
| Static dictionary, one event per frame | 43% |
| Static dictionary, 5 events per frame | 29% |

A 50-message history page gzips to 29%.

Through Caddy:

- `401` and `426` from the den pass through unchanged. The den sees the remote address as loopback, and the client's address in `X-Forwarded-For`.
- `systemctl reload caddy` and `systemctl restart caddy` close the WebSocket with 1001. The client reconnected in under 300 ms and resumed with no lost events.
- Restarting the den closed the socket with 1012, gave 502 for about 0.7 s, and then a `ready` with a new epoch.
- In the first run, the den sent 1012 before closing its listener. The client reconnected to the dying process, resumed there, and was dropped again a moment later. Closing the listener first fixed it.

### Conclusions

- Context takeover's memory (about 400 MB at 500 online) rules it out. Per-message compression alone isn't worth it for chat events.
- A shared static dictionary with per-frame deflate gets most of context takeover's saving with no per-connection state. Without takeover, a compressed frame is the same for every recipient, so the den compresses each event once. This is planned for M6. M1 sends plain JSON in frames that are arrays of events, so it fits later.
- Resume from a sequence number, with an epoch per den process, handles proxy reloads and restarts without refetching. A den restart forces a resync, which is cheap as long as clients refetch only what's on screen.
- A stopping den must close its listener before telling clients to reconnect.

## Caddy on Windows

No spike needed. Caddy v2.11.4's `service_windows.go` detects a start by the Service Control Manager, reports readiness and handles Stop, so `sc.exe create caddy binPath= "…\caddy.exe run --config …"` works without a wrapper. The M1.1 Windows den e2e runs it on the CI runner.

## Image metadata

Deferred to the start of M1.4, on real phone photos: GPS in XMP as well as EXIF, HDR gain maps appended after the main image, and orientation tags that have to survive stripping.
