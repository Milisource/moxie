# Browser bridge transport decision

Transport: long-poll HTTP over the existing loopback server (phase 0 decision).

- No hand-rolled WebSocket (RFC 6455); reuse the token model.
- Pairing: one-time code exchanged for a 32-byte pairing secret stored in
  chrome.storage.local and moxie config (0600); every request carries
  `Authorization: Bearer <secret>`.
- Host must be exactly 127.0.0.1:<port> or localhost:<port>; Origin must
  equal the pinned extension origin; reject missing Origin.
- Replace `Access-Control-Allow-Origin: *` with the pinned origin.
- Extension enforces the URL allow-list; moxie never passes arbitrary URLs.
EOF
echo done