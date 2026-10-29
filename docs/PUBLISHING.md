# GitHub 与 CPA 商店发布

此目录包含发布所需源码、文档、工作流与 registry 条目。商店安装读取公开仓库的
正式 Release。管理网页是商店入口，不是
直接上传二进制的收录接口。

## 1. 本地验证与打包

```bash
go test -race -count=1 ./...
go vet ./...
bash scripts/build.sh
python3 scripts/integration.py
python3 scripts/package.py --arch amd64 --version v2.0.0
```

原生 ARM64 上运行同样命令，将 `--arch` 改为 `arm64`，并给集成脚本加
`--library build/linux/arm64/egress-timezone.so`。
也可以按[安装指南](INSTALLATION.md#从源码构建)使用目标 C 编译器交叉构建。

输出约定：

```text
dist/
  egress-timezone_2.0.0_linux_amd64.zip
  egress-timezone_2.0.0_linux_arm64.zip
  checksums.txt
```

每个 zip 根目录恰好包含一个 `egress-timezone.so`；另含 README、示例和许可证。
`checksums.txt` 使用 sha256sum 格式。不要把旧私有部署目录、账号 JSON、真实
CPA 配置、日志、密码或令牌加入 Git。生成文件和测试结果已被 `.gitignore` 排除。

## 2. GitHub

仓库：`Renwang-Huang/cpa-egress-timezone`。
MIT 许可证署名为 Renwang Huang；上游 ABI 与路由约定的署名见
`THIRD_PARTY_NOTICES.md`，对应许可证随发行包一起分发。

把此目录的源码发布到上述仓库的 `main` 分支。发布前如果更换仓库名/作者，需同步修改
`go.mod`、插件 metadata、README、自定义源 URL 和 `store/registry.json`。

更新 `config.go` 的版本常量与 CHANGELOG 后，推送 `v2.0.0` 标签。Release 工作流：

1. 在 Ubuntu 22.04 上测试并构建 Linux amd64 / arm64 glibc 共享库。
2. 在 amd64 上运行隔离 CPA 集成测试，打包两种架构。
3. 合并 SHA-256 校验和，创建 **draft GitHub Release**，使用
   `docs/RELEASE_NOTES.md` 作为正文，等待维护者检查后手动发布。

草稿和 prerelease 不作为商店 latest 正式版本。维护者发布正式 Release 后，
上游安装器才可以读取最新版本及附件。以后推送新正式 Release 就能提供更新，
不需要为每次升级修改官方 registry 的版本字段。

## 3. 自定义商店源

GitHub 仓库公开、源码合并到 main、正式 Release 发布后，添加：

```yaml
plugins:
  enabled: true
  store-sources:
    - https://raw.githubusercontent.com/Renwang-Huang/cpa-egress-timezone/main/store/registry.json
```

在 CPA 管理页「插件 → 商店」刷新并安装。检查实际 `effective_enabled`、metadata
版本、平台库路径和插件状态接口，然后用受控请求核对时区。自己的管理页域名只改变
访问入口，不改变 registry 与 Release 格式。

## 4. 官方商店收录 PR

上游仓库：
[router-for-me/CLIProxyAPI-Plugins-Store](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store)。
根据其当前 README，把本地 `store/registry.json` 中唯一的插件对象追加到上游
`registry.json` 的 `plugins` 数组，保留原有条目与排序，提交 PR。官方源只维护
metadata；二进制、校验和与 Release 说明保留在本项目仓库。

PR 标题建议：`Add Egress Timezone plugin`。

PR 正文草稿：

> Adds `egress-timezone` by Renwang-Huang (MIT). Adapts supported Codex/OpenAI
> environment timezone declarations to a cached egress-IP observation through
> the selected credential's proxy. Preserves requests on lookup failure.
> Source, Linux amd64/arm64 release assets, checksums, design limitations and
> isolated integration instructions are maintained in the plugin repository.

提交前确认仓库 URL 与正式 Release 附件可公开访问。合并后才会在官方源中出现。

格式依据：[官方商店 README](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store#release-requirements)。
