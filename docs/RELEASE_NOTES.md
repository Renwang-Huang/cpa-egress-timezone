# Egress Timezone 2.0.0

Automatic timezone normalization for Codex/OpenAI requests based on the selected
CPA credential's observed egress IP.

- After-auth route selection; legacy and grouped CPA v8 configuration.
- Short route observations plus separate IP-to-timezone caching.
- Concurrent lookup coalescing, bounded time/memory, safe failure behavior.
- HTTP, streaming, Chat Completions and WebSocket integration coverage.
- Embedded IANA tzdata and selective current-date adjustment.
- Authenticated diagnostics and explicit fixed-timezone mode.
- Linux amd64/arm64 glibc packages with SHA-256 checksums and third party notices.

Requires a CGO/plugin-enabled CPA. Tested compatibility baseline: v8.0.47.
Disable the earlier `los-angeles-timezone` plugin before enabling this plugin.

Automatic mode assumes a stable route shared by probes and model requests.
Per-connection rotation, unexposed SDK proxy overrides, custom transports and
additional upstream gateways prevent exact per-model-connection IP attribution.
See `docs/DESIGN.md` before enabling it in those deployments.

First/expired lookups can add up to the configured budget (default four seconds).
Failed lookups leave original requests unchanged unless a fixed fallback is
explicitly configured. GeoIP provider terms, quotas and data accuracy apply.

Verify `checksums.txt` and choose the archive matching your CPA runtime's CPU and
libc. This release does not include musl, macOS or Windows builds.
