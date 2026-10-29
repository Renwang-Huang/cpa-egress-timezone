# Configuration

[← Project home](../README.en.md) · [简体中文](CONFIGURATION.md) · [Complete example](../examples/plugin-config.yaml)

## Configuration location

Settings live under `plugins.configs.egress-timezone`. CPA's management UI can also display the fields declared by the plugin. Merge settings into your existing configuration.

```yaml
plugins:
  enabled: true
  configs:
    egress-timezone:
      enabled: true
      priority: 1000
      mode: auto
      cpa_config_path: /CLIProxyAPI/config.yaml
```

`enabled` and `priority` are CPA plugin loader settings. The options below belong to this plugin.

## All options

| Option | Default | Description |
| :--- | :--- | :--- |
| `mode` | `auto` | `auto` or `fixed` |
| `cpa_config_path` | Discovered | Read `--config` / `-config`, otherwise `./config.yaml`; use the path inside CPA's container |
| `probe_url` | Empty | Probe the upstream origin's `/cdn-cgi/trace` by default; a custom endpoint must follow the model request's exit rules |
| `geoip_url` | `https://ipwho.is/{ip}` | Must contain exactly one `{ip}` placeholder |
| `cache_ttl_seconds` | `60` | Route observation TTL, 1–86400 seconds; expired observations are not used for rewriting |
| `geoip_cache_ttl_seconds` | `21600` | IP → timezone TTL, 1–604800 seconds; independent of route probing |
| `retry_seconds` | `10` | Failure retry cooldown, 1–3600 seconds |
| `lookup_timeout_ms` | `4000` | Total budget for a probe and any required GeoIP lookup, 100–15000 milliseconds |
| `fallback_timezone` | Empty | Preserve the request on failure; set an IANA timezone for an explicit fallback |
| `fixed_timezone` | `America/Los_Angeles` | Used only in `fixed` mode, without probe or GeoIP requests |
| `address_family` | `auto` | `auto` / `ipv4` / `ipv6`; forced families apply only to direct connections; explicit proxies require `auto` |
| `max_cached_routes` | `128` | Capacity of each route/GeoIP cache, 1–4096 |
| `allow_http` | `false` | Allow plaintext probe/origin endpoints; intended only for controlled local testing |

## Fixed timezone mode

Use fixed mode when automatic observation is unnecessary:

```yaml
plugins:
  enabled: true
  configs:
    egress-timezone:
      enabled: true
      priority: 1000
      mode: fixed
      fixed_timezone: America/Los_Angeles
```

Fixed mode makes no probe or GeoIP requests. Embedded IANA tzdata handles daylight saving transitions.

## Probe and GeoIP endpoints

Probe responses may use Cloudflare trace's `ip=...` format or `{"ip":"public IP"}`. GeoIP responses must return the same `ip` and either `timezone.id` or a string `timezone`. The plugin does not infer timezones from country abbreviations.

The default GeoIP provider requires internet access and has quotas and availability limits. See the [provider's documentation](https://ipwhois.io/documentation). A compatible service of your own is supported. Only the observed public IP is submitted; account tokens, request bodies, cookies, and client identity headers are not forwarded.

Custom upstreams without a trace endpoint need a `probe_url` with routing equivalent to model requests. Otherwise automatic mode preserves the request by default. See [design and accuracy limits](DESIGN.md) (中文).

## File access

The plugin must be able to read CPA's configuration inside its runtime. OAuth Codex accounts also require the selected credential JSON under `oauth.auth-dir` (legacy: `auth-dir`). Only routing fields are read, and file contents are not sent to probe endpoints.

Encrypted files, dynamic accounts, non-file credentials delivered by Home, or insufficient permissions cause automatic mode to skip rewriting. An unreadable credential is not treated as a global route.

## Status and diagnostics

Custom plugin routes use the `v0` prefix and require CPA management authentication:

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  http://127.0.0.1:8317/v0/management/plugins/egress-timezone/status
```

| Field | Meaning |
| :--- | :--- |
| `observations[].ip` / `timezone` | Observed IP and resolved timezone |
| `observations[].observed_at` / `expires_at` | Observation timestamp and expiry |
| `observations[].last_error` | Sanitized probe or lookup error |
| `last_skip_reason` | Most recent reason for skipping a rewrite |
| Counters | Request handling, rewriting, and lookup activity |

Historical observations remain visible after failure but are not used for expired-cache rewriting. Caches exist only in memory and clear on configuration reload. Account and probe records are not persisted. Status includes public IPs; protect it with CPA management authentication.

## Troubleshooting

### Why is the first request, or a request after expiry, slower?

Cold or expired observations trigger synchronous probing, with a default total budget of four seconds. Concurrent lookups for the same route are coalesced. Valid cached routes are reused; there is no background polling. Adjust `lookup_timeout_ms` to your latency budget.

### Why did the exit change without an immediate timezone update?

Route observations are valid for 60 seconds by default. The next request after expiry refreshes them. GeoIP mappings are cached independently for six hours; a new IP uses its own mapping without waiting for the old IP's TTL.

### Why was the request left unchanged?

Check `last_skip_reason` and `last_error`. Verify the active plugin, readable configuration and credentials, a supported selected account, and reachable trace/GeoIP endpoints. Failed refreshes preserve the request by default. Set `fallback_timezone` only when an explicit fallback is wanted.

### Can I force IPv4 or IPv6?

`address_family` constrains direct probe connections only. An explicit proxy combined with a non-`auto` family causes a skip. This option cannot change the proxy's or CPA model connection's address selection.
