# Publication API v1

PUT /api/v1/monitors takes application/json and one Authorization bearer credential. A complete snapshot replaces the authenticated client's declaration. A missing monitors property is invalid. An empty object intentionally declares zero monitors. The server rejects the entire body on any error. Subsequent successful requests from one client supersede earlier ones in server commit order.

```json
{"schemaVersion":1,"monitors":{"site":{"type":"keyword","name":"Site health","url":"https://web.example.org/health","keyword":"Healthy","interval":60}}}
```

Use HTTPS with ordinary certificate verification. A curl client can construct a header on standard input. The token file contains the raw token with an optional final newline.

```sh
{ printf 'Authorization: Bearer '; tr -d '\r\n' < /run/secrets/magickuma-token; printf '\n'; } |
  curl --fail-with-body --max-time 30 --proto '=https' -X PUT \
    -H @- -H 'Content-Type: application/json' --data-binary @monitors.json \
    https://monitoring.example.org/api/v1/monitors
```

Successful acceptance returns 200 with changed and acceptedAt. Repeated equivalent snapshots update freshness. Acceptance certifies durable publication only. Clients must never infer that a check has run. Errors use an error code and message, with a monitor/property path for validation failures. Use 400 for malformed JSON, 401 for invalid credentials, 413 for oversized bodies, 415 for media type, 422 for schema/policy errors and 503 for storage problems. Retry transport and server failures with backoff. Diagnose policy errors before retrying.

The receiver admits up to 16 simultaneous authenticated publication bodies per HTTP handler. At capacity it returns 429 with Retry-After. Retry with jitter. The per-request 1 MiB limit still applies. For a large or exposed installation, rate-limit the publication route at the TLS proxy to bound repeated writes per source. A monitor's interval governs normal scheduled checks; Kuma can execute an initial check after a configuration edit.
