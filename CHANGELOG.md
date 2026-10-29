# Changelog

## 2.0.0

- Add automatic timezone selection from the egress IP observed through the
  selected CPA credential's proxy, using the upstream origin's trace endpoint.
- Apply automatic rewriting in the after-auth request hook so account proxy
  selection and WebSocket frames are handled.
- Support Codex file credentials, Codex API keys and OpenAI compatibility
  credentials, including legacy and grouped v8 configuration layouts.
- Separate short route observations from IP-to-timezone caching; coalesce
  concurrent lookups and bound memory, response sizes and lookup latency.
- Preserve requests when lookup fails; never use an expired route observation.
- Retain an explicit fixed-timezone mode and optional fixed fallback.
- Add management diagnostics, isolated integration tests, release packaging,
  a store registry entry and Linux amd64/arm64 build automation.
- Keep selective environment/date normalization, embedded tzdata, number
  precision, opaque payloads and unrelated identity headers.

## Earlier private plugin

The earlier `los-angeles-timezone` 1.1.0 plugin used a fixed Los Angeles timezone.
It is a separate plugin ID. Disable it when migrating to `egress-timezone`.
