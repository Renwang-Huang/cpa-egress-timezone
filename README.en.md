# CPA Egress Timezone

Adapt supported Codex / OpenAI client timezone declarations to the IANA timezone
of the selected CPA credential's observed egress IP.

[中文说明](README.md) · [Configuration](examples/plugin-config.yaml) ·
[Design and limits](docs/DESIGN.md) · [Publishing](docs/PUBLISHING.md)

Plugin ID: `egress-timezone`. Version: `2.0.0`. License: MIT.
Compatibility baseline: CPA `v8.0.47`, C ABI v1, plugin schema v2, with CGO
dynamic plugins enabled. Linux amd64 and arm64 glibc are release targets.

## How it works

The after-auth hook receives `selected_auth_id`. The plugin reads the local CPA
configuration and, for file-backed Codex accounts, the selected auth file to
recover the upstream origin and credential proxy. Both legacy and grouped v8
configuration layouts are supported.

It probes the upstream origin's `/cdn-cgi/trace` through that proxy, then asks a
GeoIP endpoint for the timezone of the **observed public IP**. Route observations
are short lived; IP-to-timezone mappings are cached separately. Expired route
observations are never used to rewrite a request after a failed refresh.

The probe is a separate connection. Per-connection rotation, destination/path
routing, SDK request proxy overrides, custom transports and another forwarding
gateway can make the model request's actual exit differ. This plugin cannot
attest the source IP of every model connection. Use automatic mode only when
probe and model traffic share a stable route. See [limits](docs/DESIGN.md).

## Selective rewriting

- Rewrite timezone tags inside supported client environment blocks, explicit
  client timezone fields and existing timezone headers.
- Adjust only the latest unambiguous environment `current_date` when it equals
  today in the original IANA timezone. Embedded tzdata handles DST.
- Preserve ordinary text, tool arguments/results, encrypted reasoning, assistant
  history, historical dates, numeric precision and unrelated identity headers.
- Do not change host/container timezone, credentials, proxies or system routes.

## Build and use

Install Go (the toolchain is pinned in `go.mod`) and a C compiler:

```bash
go test -race -count=1 ./...
go vet ./...
bash scripts/build.sh
```

Copy `build/linux/<arch>/egress-timezone.so` to CPA's plugin directory and merge
[the example](examples/plugin-config.yaml) into its existing configuration.
`cpa_config_path` must be readable inside CPA's runtime/container. Do not replace
your full configuration with the example. Disable the older
`los-angeles-timezone` plugin before enabling this one.

Automatic mode defaults to a 60-second route observation, a six-hour GeoIP
mapping cache and a four-second total lookup budget. On failure the original
request is preserved. Set `fallback_timezone` only if you want an explicit fixed
fallback. `mode: fixed` uses `fixed_timezone` and performs no network lookup.

Default GeoIP: `https://ipwho.is/{ip}`. Public provider quotas and terms apply.
Only the observed IP is sent; model bodies, provider credentials, cookies and
identity headers are not forwarded. A compatible private service is supported.
Custom upstreams without a trace endpoint need a configured, route-equivalent
`probe_url`; otherwise auto mode leaves requests unchanged.

## Verify and package

```bash
python3 scripts/integration.py
python3 scripts/package.py --arch amd64
```

Integration runs an isolated CPA container, mock upstream and two controlled
proxies using fake credentials. It covers HTTP, streaming, Chat Completions,
WebSocket, route isolation, exit changes, cache reuse, lookup failure and client
identity preservation against a disabled-plugin baseline. Synthetic observations
are not evidence of production egress. No real model API is invoked.

An opt-in live probe is available with `CPA_EGRESS_LIVE_CONFIG` and
`go test -run '^TestLiveProbeOptIn$' -v`. Run it in CPA's network and filesystem
namespace. It requests trace/GeoIP only; normal CI skips it. See
[verification evidence](docs/VERIFICATION.md), including a real timeout and its
subsequent successful recheck.

Authenticated management diagnostics:
`GET /v0/management/plugins/egress-timezone/status`.
It includes observation timestamps, expiry, timezone, IP and sanitized failure
reasons. Historical observations remain visible after a failure but are not
used for expired-cache rewriting. Configuration reload clears the in-memory
caches.

Source: [Renwang-Huang/cpa-egress-timezone](https://github.com/Renwang-Huang/cpa-egress-timezone).
Install a formal [GitHub Release](https://github.com/Renwang-Huang/cpa-egress-timezone/releases)
or use the custom store source in the Chinese README. Official store listing
depends on the upstream registry pull request being merged.
See [publishing instructions](docs/PUBLISHING.md) and
[third party notices](THIRD_PARTY_NOTICES.md).
