# 安装与迁移

[← 项目首页](../README.md) · [English](INSTALLATION.en.md) · [配置参考](CONFIGURATION.md)

## 兼容性

| 项目 | 要求 |
| :--- | :--- |
| CPA | 支持 CGO 动态插件；兼容基线 `v8.0.47` |
| 插件接口 | C ABI v1 / 插件协议 v2 |
| 发行平台 | Linux amd64 / arm64，glibc |
| 源码构建 | Go（`go.mod` 固定工具链 `go1.27.3`）及 C 编译器 |

尚未提供 macOS、Windows 或 musl 构建。glibc 发行包不能直接用于 Alpine / musl，运行环境也必须满足二进制的 glibc 版本要求。

## 从商店安装

参见首页的[快速开始](../README.md#快速开始)，添加本仓库的自定义源，再到 CPA 管理页「插件 → 商店」安装 **Egress Timezone**。

自定义源读取本仓库最新的正式 Release。官方源是否已收录，以[上游 registry](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store)为准；自定义源不依赖官方收录。商店的发布流程见 [PUBLISHING.md](PUBLISHING.md)。

## 手动安装发行包

从 [GitHub Releases](https://github.com/Renwang-Huang/cpa-egress-timezone/releases/latest) 下载与你的平台对应的 zip 及 `checksums.txt`。以 `v2.0.0` 为例：

```text
egress-timezone_2.0.0_linux_amd64.zip
egress-timezone_2.0.0_linux_arm64.zip
checksums.txt
```

在下载目录中校验已下载的文件：

```bash
sha256sum --check --ignore-missing checksums.txt
```

解压后，将 `egress-timezone.so` 复制到 CPA 对应平台的插件目录，例如容器内 `plugins/linux/arm64/egress-timezone.so`。把[示例配置](../examples/plugin-config.yaml)合并到现有 CPA 配置，并使用运行环境内的真实配置路径。

## 从源码构建

安装 Go 和 C 编译器，然后运行：

```bash
git clone https://github.com/Renwang-Huang/cpa-egress-timezone.git
cd cpa-egress-timezone
bash scripts/build.sh
```

原生构建输出：`build/linux/<架构>/egress-timezone.so`。安装方式与手动发行包相同。

交叉编译 ARM64 需要目标平台 C 编译器：

```bash
CC=aarch64-linux-gnu-gcc GOOS=linux GOARCH=arm64 bash scripts/build.sh
```

## 从旧插件迁移

旧 ID 为 `los-angeles-timezone`，新 ID 为 `egress-timezone`。

1. 备份现有 CPA 配置和旧 `.so`。
2. 禁用旧插件，安装并启用新插件，避免两个插件先后覆盖结果。
3. 检查实际激活版本、路径与状态，并通过受控请求确认改写。

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

原地覆盖已经加载的同一路径 `.so` 不保证热重载。商店升级后检查实际激活版本，必要时在维护窗口重启 CPA。回滚时禁用 `egress-timezone`，恢复旧插件及已备份配置。

## 开发验证

```bash
go test -race -count=1 ./...
go vet ./...
bash scripts/build.sh
python3 scripts/integration.py
python3 scripts/package.py --arch amd64
```

集成测试启动独立 CPA 容器、本地假上游和两条可控代理，使用假密钥。覆盖普通/流式 Responses、Chat Completions、WebSocket、多账号出口隔离、IP 变化、缓存、失败保留原请求，以及与禁用插件基线一致的身份请求头。

测试不调用真实模型 API；合成 IP / GeoIP 结果不能视为生产出口验证。结果写入忽略目录 `build/`。

如需显式进行真实 trace/GeoIP 补测，在与 CPA 相同的网络及文件系统环境中运行：

```bash
CPA_EGRESS_LIVE_CONFIG=/CLIProxyAPI/config.yaml \
  go test -run '^TestLiveProbeOptIn$' -v
```

可以另设 `CPA_EGRESS_LIVE_AUTH_ID` 指定凭据；否则选择第一个可读、未禁用的 Codex JSON 文件。该测试不调用模型 API。正常 CI 不设置这些变量，不会访问真实账号。已记录的验证结果及首次网络超时见 [VERIFICATION.md](VERIFICATION.md)。
