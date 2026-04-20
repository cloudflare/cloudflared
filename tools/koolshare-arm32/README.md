# cloudflared arm32 (koolshare)

这个目录提供一个**独立产出目录**，用于生成可在 koolshare 路由器软件中心分发的 Cloudflare Tunnel (cloudflared) arm 32 位安装包。

## 目录结构

- `build_koolshare_package.sh`: 一键构建和打包脚本
- `package/`: 安装模板（安装/启动/停止脚本、配置模板）
- `output/`: 构建产物输出目录（按版本分目录）

## 构建

在仓库根目录执行：

```bash
./tools/koolshare-arm32/build_koolshare_package.sh
```

也可以手动指定版本号（用于软件中心展示版本）：

```bash
./tools/koolshare-arm32/build_koolshare_package.sh 2026.4.20-1
```

脚本会做以下事情：

1. 使用 `TARGET_OS=linux TARGET_ARCH=arm TARGET_ARM=7 make cloudflared` 构建 ARM 32 位二进制。
2. 按 koolshare 结构组织文件（`bin/`、`scripts/`、`config/`）。
3. 生成 tar.gz 包和 `sha256` 文件，产出到 `tools/koolshare-arm32/output/<version>/`。

## 软件中心安装建议

将生成的 `cloudflared-koolshare-arm32-<version>.tar.gz` 作为插件包上传后，安装脚本应调用：

```bash
scripts/install.sh
```

安装后默认路径：

- 二进制：`/koolshare/bin/cloudflared`
- 配置：`/koolshare/configs/cloudflared/config.yml`
- 日志：`/koolshare/var/log/cloudflared.log`

运行控制：

- 启动：`scripts/start.sh`
- 停止：`scripts/stop.sh`
- 卸载：`scripts/uninstall.sh`

## Tunnel 使用示例

在 Cloudflare Zero Trust 创建 Tunnel，并将 credentials json 上传到：

`/koolshare/configs/cloudflared/<tunnel-uuid>.json`

然后在 `config.yml` 里配置你的域名，如 `router.example.com -> http://127.0.0.1:80`，启动后即可通过域名访问路由器内网服务。
