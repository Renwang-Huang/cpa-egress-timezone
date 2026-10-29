# Third party notices

## CLIProxyAPI

The C ABI declarations, credential ID convention (`stableID`, sorted headers),
and grouped v8 routing configuration semantics are adapted from CLIProxyAPI.

- Source: https://github.com/router-for-me/CLIProxyAPI/tree/v8.0.47
- Relevant paths: `sdk/pluginabi`, `examples/plugin`,
  `internal/watcher/synthesizer/helpers.go`, `internal/config/config_v8.go`
- License: MIT

```text
MIT License

Copyright (c) 2025-2005.9 Luis Pater
Copyright (c) 2025.9-present Router-For.ME

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Runtime dependencies

Dependency versions and integrity hashes are pinned in `go.mod` and `go.sum`.

| Component | License | Source |
| --- | --- | --- |
| Go runtime and embedded IANA tzdata package | BSD 3 Clause; timezone database data has its own public domain notices | https://go.dev/LICENSE |
| golang.org/x/net/proxy | BSD 3 Clause | https://cs.opensource.google/go/x/net/+/master:LICENSE |
| gopkg.in/yaml.v3 | MIT and Apache 2.0 components | https://github.com/go-yaml/yaml/tree/v3.0.1 |

Release packages include the applicable upstream license files in `licenses/`.
GeoIP is queried over HTTP and no GeoIP database is bundled. The selected
provider's terms apply independently.
