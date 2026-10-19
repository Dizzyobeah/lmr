# Factory backlog

`/factory next` runs the first unchecked item and ticks it.

- [x] Rate limiter: apply a changed per-key rate limit without a restart (windows keep the limit they were created with in internal/middleware/ratelimit.go) (001-live-rate-limits)
- [x] Admin: show each API key's last-used time (keystore already stores last_used_at) in the API Keys table (002-key-last-used)
- [x] Embeddings: count tokens from Ollama's response instead of the len/4 estimate (003-embedding-token-counts)
- [x] Config: make Ollama num_ctx configurable instead of hard-coded 32768 in internal/server/server.go (004-configurable-num-ctx)
- [x] Admin: HTML-escape API key names (and other user values) in handleListKeys; key.Name is written into the table unescaped (internal/admin/handler.go) (005-escape-key-names)
- [ ] Server: cap request body size on /v1/* routes (no http.MaxBytesReader anywhere in internal/), returning an OpenAI-style 413
- [ ] Admin: admin.html hardcodes /admin/api/... htmx URLs (and other /admin paths), so the dashboard breaks when admin.path is not /admin; use the configured AdminPath (found in job 006 review)
- [ ] Admin: add SRI integrity hashes (or self-host) for the htmx and Alpine scripts loaded from unpkg in admin.html and chat.html
