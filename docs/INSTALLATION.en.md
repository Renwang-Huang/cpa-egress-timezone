# Installation & migration

[← Project home](../README.en.md) · [简体中文](INSTALLATION.md) · [Configuration](CONFIGURATION.en.md)

## Compatibility

| Component | Requirement |
| :--- | :--- |
| CPA | CGO dynamic plugins enabled; compatibility baseline `v8.0.47` |
| Plugin interface | C ABI v1 / plugin schema v2 |
| Release platforms | Linux amd64 / arm64, glibc |
| Source builds | Go (`go.mod` pins toolchain `go1.27.3`) and a C compiler |

No macOS, Windows, or musl builds are provided. glibc release binaries cannot be used directly on Alpine / musl. The runtime must also satisfy the binary's glibc version requirements.

## Install from the store

Follow the [quick start](../README.en.md#quick-start) to add this repository's custom source, then install **Egress Timezone** from **Plugins → Store** in CPA's management UI.

The custom source reads this repository's latest formal Release. Official listing depends on the [upstream registry](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store); the custom source works independently of that listing. See [publishing instructions](PUBLISHING.md) (中文).

## Install a release manually

Download the zip for your platform and `checksums.txt` from [GitHub Releases](https://github.com/Renwang-Huang/cpa-egress-timezone/releases/latest). For `v2.0.0`:

```text
egress-timezone_2.0.0_linux_amd64.zip
egress-timezone_2.0.0_linux_arm64.zip
checksums.txt
```

Verify the files you downloaded:

```bash
sha256sum --check --ignore-missing checksums.txt
```

Extract and copy `egress-timezone.so` to CPA's platform plugin directory, for example `plugins/linux/arm64/egress-timezone.so` inside its container. Merge [the example](../examples/plugin-config.yaml) into CPA's existing configuration using its runtime configuration path.

## Build from source

Install Go and a C compiler, then run:

```bash
git clone https://github.com/Renwang-Huang/cpa-egress-timezone.git
cd cpa-egress-timezone
bash scripts/build.sh
```

Native builds produce `build/linux/<arch>/egress-timezone.so`. Install it as described for a release package.

Cross-compiling ARM64 requires a target C compiler:

```bash
CC=aarch64-linux-gnu-gcc GOOS=linux GOARCH=arm64 bash scripts/build.sh
```

## Migrate from the older plugin

The old ID is `los-angeles-timezone`; the new ID is `egress-timezone`.

1. Back up CPA's current configuration and the old `.so`.
2. Disable the old plugin before installing and enabling this one.
3. Check the active version, path, and status, then verify rewriting with a controlled request.

```yaml
plugins:
  enabled: true
  configs:
    los-angeles-timezone:
      enabled: false
    egress-timezone:
      enabled: true
      priority: 1000
      mode: auto
      cpa_config_path: /CLIProxyAPI/config.yaml
```

Overwriting an already loaded `.so` at the same path does not guarantee a hot reload. Check the active version after store upgrades and restart CPA in a maintenance window if required. To roll back, disable `egress-timezone` and restore the backed-up plugin and configuration.

## Development verification

```bash
go test -race -count=1 ./...
go vet ./...
bash scripts/build.sh
python3 scripts/integration.py
python3 scripts/package.py --arch amd64
```

Integration starts an isolated CPA container, mock upstream, and two controlled proxies with fake credentials. It covers regular/streaming Responses, Chat Completions, WebSocket, account route isolation, IP changes, caching, failure preservation, and identity headers matching a disabled-plugin baseline.

No real model API is called. Synthetic IP / GeoIP observations do not verify production egress. Results are written under the ignored `build/` directory.

For an explicit live trace/GeoIP check, run in CPA's network and filesystem environment:

```bash
CPA_EGRESS_LIVE_CONFIG=/CLIProxyAPI/config.yaml \
  go test -run '^TestLiveProbeOptIn$' -v
```

Optionally set `CPA_EGRESS_LIVE_AUTH_ID`; otherwise the test selects the first readable, enabled Codex JSON file. It does not call model APIs. Normal CI leaves these variables unset and skips real accounts. See [verification evidence](VERIFICATION.md) (中文), including the initial network timeout.
