# Smart 能力移植记录

日期：2026-09-30。来源：相邻仓库 sub2_smart 的 production 分支，提交 9f7d96be367e795bd76e1f45c82bd5d4c563e0fa。

本次以当前项目提交 ff1a4bac451bd40bef5ce21d9c610741c83be363 为基线，将 Smart 的新增能力合并到现有架构。移植分支为 codex/port-smart-capabilities；本次只创建本地提交，未推送或生产部署。

## 新增能力

| 领域 | 移植内容 |
| --- | --- |
| Excel / BPS | 网关路由、图片与工具调用、会话恢复、预热池、403 恢复与质量联动 |
| Smart 运维 | 账号质量巡检、定时题库检测、自动判分、自动隔离/恢复、规则模板、运营事件与告警 |
| 账号维护 | Token Guard、2FA 凭据导入、OAuth 重新登录、托管/外置 worker 与代理配置 |
| 调度与定价 | 优先级/利润信号调度、OAuth 调度倍率、分组内账号倍率、推理等级价卡、RPM 准入 |
| 范围控制 | 账号分组模型限制、用户分组禁用模型、仅流式分组、观察员及授权分组 |
| 请求诊断 | 请求阶段耗时、请求捕获、请求/响应模型观察与排错界面 |
| 质量展示 | Pelican 定时测试、分组测试与成本、展示页、监控 candy 指标 |
| 上游适配 | Copilot SDK sidecar、DeepSeek compact、WebSocket/SSE、Codex 票据与 Mihomo/780 采集 |
| 多实例 | full/gateway 运行角色、readiness、优雅撤流、Kubernetes 请求副本示例 |

## 保留的本项目能力

企业组织/IAM、企业订阅计费、OIDC、Kiro、媒体/COS、客服工单与 RAG、原有渠道定价和图片输入计费继续保留。用户先前在管理设置 API 和 SettingsView 中的客服配置修改也予以保留。

合并后补齐了 Ent / Wire 生成代码及依赖注入，修正重复路由、重复倍率、请求推理等级记录和重复票据注入。图片任务在客户端断连后仍继续完成，同时保留账号准入和分组权限检查。

## 数据库迁移

原项目已存在的迁移保持原编号和内容。Smart 的 27 个独立迁移重编号为 251–277，避免与企业/IAM 等既有迁移重名；已存在的内容审核元数据、推理倍率和联盟账本 operation_id 迁移不重复导入。

上线前先备份数据库，并在预发使用完整迁移链验证。应用版本回退不等于数据库回退；不得直接删除迁移记录或盲目重放旧 SQL。

## 运行前提

- 重新登录的托管运行包仅支持 Linux amd64 / arm64，必须有与服务版本匹配的 Release asset。详见 tools/reauth-runtime/README.md。开发版、旧 Release 或其他系统可使用独立部署的外置 worker。
- 发布配置已改为本仓库的 Release 源；完整发布附带双架构运行包，Simple Release 附带 amd64 包。本次没有上传运行包或触发工作流。
- BPS、Mihomo/采集及 Copilot sidecar 需按各自配置准备账号、出口、运行时和可达服务；代码移植不会复制另一仓库的生产凭据或数据库。
- gateway 副本需共用 PostgreSQL、Redis 和密钥；BPS/图片、WS、本地代理等进程状态的路由要求见 deploy/kubernetes/README.md。示例使用占位镜像标签，部署时必须替换为已验证的镜像摘要。

## 验证记录

所有数据库集成测试使用本机临时 PostgreSQL/Redis 容器，不连接现有业务数据库。
提交前已重新执行 AGENTS.md 要求的前端、审计例外、后端单元/集成、漏洞和静态检查，全部通过。完整前端测试中的既有失败另行列明。

已验证：

- go test -tags=unit ./...：全量通过，包含原项目与移植功能的后端单元测试。
- go test -tags=integration ./...：全量通过；27 个新增迁移、质量隔离/恢复、分组限制、凭据与请求数据持久化在隔离数据库中验证。
- golangci-lint run：0 issues（使用 Go 1.27 编译的 v2.12.2）。
- make test-frontend：427 个 Vue SFC 语法检查、ESLint、TypeScript 和 22 个关键测试文件全部通过，共 353 项测试。
- pnpm --dir frontend run build：通过，构建产物可嵌入后端。部分 chunk 超过 500 kB，属于构建警告。
- 前端完整测试：455 个文件中 433 个通过、22 个失败；3,651 项通过、44 项失败。剩余失败已与移植前基线对照，全部为原有失败（主要是旧测试数据/模块 mock、企业/IAM、平台目录和 i18n 编译断言）。本次引入的前端回归已修复，没有通过跳过测试掩盖失败。
- 依赖例外检查通过：python3.11 tools/check_pnpm_audit_exceptions.py --audit frontend/audit.json --exceptions .github/audit-exceptions.yml。系统 Python 3.9 不支持脚本的联合类型标注，需 Python 3.10+。
- govulncheck ./...：未发现可达漏洞；另提示导入包和依赖模块存在未被当前调用路径触达的漏洞。
- Python worker：43 项测试，42 项通过、1 项可选运行环境测试跳过；本机未构建 Linux 双架构运行包，不以本地 Python 测试代替发布包验证。
- go build ./... 及 go build -tags=embed ./cmd/server：通过。
- Release / GoReleaser 工作流 YAML 解析和 git diff --check：通过。

本机 Docker 使用 Colima。运行数据库测试时需设置 DOCKER_HOST=unix:///Users/jiantaoli/.colima/default/docker.sock 和 TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock；CI 使用其自身 Docker 连接。所有原有 SQL 迁移文件没有修改。

系统 golangci-lint 使用 Go 1.26 构建，无法检查本项目 Go 1.27；验证使用临时目录中以 Go 1.27 重编译的同版本 v2.12.2，不替换系统安装。

## 迁移编号映射

| Smart 文件 | 本项目文件 |
| --- | --- |
| 239_codex_harvest_node_learning.sql | 251_codex_harvest_node_learning.sql |
| 240_codex_harvest_flow_events.sql | 252_codex_harvest_flow_events.sql |
| 241_add_account_group_rate_multiplier.sql | 253_add_account_group_rate_multiplier.sql |
| 242_request_timing_details.sql | 254_request_timing_details.sql |
| 243_pelican_scheduled_tests.sql | 255_pelican_scheduled_tests.sql |
| 244_account_group_allowed_models.sql | 256_account_group_allowed_models.sql |
| 245_user_group_denied_models.sql | 257_user_group_denied_models.sql |
| 246_group_stream_only.sql | 258_group_stream_only.sql |
| 247_account_quality_ops.sql | 259_account_quality_ops.sql |
| 248_account_quality_judgment.sql | 260_account_quality_judgment.sql |
| 249_account_ops_alerts.sql | 261_account_ops_alerts.sql |
| 249_pelican_showcase_items.sql | 262_pelican_showcase_items.sql |
| 250_account_token_guard.sql | 263_account_token_guard.sql |
| 251_request_captures.sql | 264_request_captures.sql |
| 252_user_observer_groups.sql | 265_user_observer_groups.sql |
| 253_pelican_group_tests.sql | 266_pelican_group_tests.sql |
| 254_channel_monitor_v2_candy.sql | 267_channel_monitor_v2_candy.sql |
| 254_openai_oauth_reauth.sql | 268_openai_oauth_reauth.sql |
| 254_quality_bps_coexist.sql | 269_quality_bps_coexist.sql |
| 255_account_token_guard_v2.sql | 270_account_token_guard_v2.sql |
| 256_openai_oauth_reauth_proxy_override.sql | 271_openai_oauth_reauth_proxy_override.sql |
| 257_openai_oauth_reauth_proxy_source.sql | 272_openai_oauth_reauth_proxy_source.sql |
| 258_quality_observation_scope.sql | 273_quality_observation_scope.sql |
| 259_account_auto_config_events.sql | 274_account_auto_config_events.sql |
| 260_quality_rule_templates.sql | 275_quality_rule_templates.sql |
| 261_pelican_group_test_costs.sql | 276_pelican_group_test_costs.sql |
| 262_openai_oauth_reauth_engine.sql | 277_openai_oauth_reauth_engine.sql |
