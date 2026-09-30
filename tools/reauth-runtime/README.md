# 托管 OAuth 重新登录运行包

后端托管 worker 仅支持 Linux amd64 / arm64；macOS、Windows 会报告 unsupported_platform，可通过单独部署 worker 使用重新登录功能。

运行包按服务版本匹配，从本仓库同名 v<version> Release 下载 sub2api-reauth_<version>_linux_<arch>.tar.gz，并校验 GitHub Release asset 的 SHA-256 digest 后解压执行。没有匹配资产的开发版或旧 Release 无法自动安装运行包。

构建需要 Docker Buildx；跨架构构建需要 QEMU。源码与依赖版本由 Dockerfile、requirements.txt、package-lock.json 锁定。

```bash
VERSION=1.2.3 ARCH=amd64 bash tools/reauth-runtime/build.sh
VERSION=1.2.3 ARCH=arm64 bash tools/reauth-runtime/build.sh
```

release.yml 完整发布会构建并上传两个架构；Simple Release 上传 amd64 运行包。GoReleaser 将 ReleaseRepository 设置为当前 GitHub 仓库。自行构建分叉仓库时请通过 Go ldflags 的 -X github.com/Wei-Shaw/sub2api/internal/reauthruntime.ReleaseRepository=owner/repository 指定发布源。

reauth-runtime.yml 只构建和验证临时 artifact，不发布 Release。本次移植仅修改发布配置，未执行发布。
