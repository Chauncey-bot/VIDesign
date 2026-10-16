# Packaging AI

AI Agent 驱动的产品包装设计系统，后端使用 Go。

## 当前内容

- `cmd/server`：HTTP 服务入口
- `internal/domain`：领域模型
- `internal/agent`：LLM、图片生成和领域 Agent 接口
- `internal/workflow`：需求采集工作流和项目状态推进
- `internal/taskqueue`：Redis + Asynq 异步任务队列
- `internal/httpapi`：第一版 HTTP 路由骨架
- `migrations/001_init.sql`：MySQL 8.0.16+ 数据库结构
- `docs/API.md`：API v1 文档

## 本地运行

```bash
cp .env.example .env
# 编辑 .env，填写 LLM_API_KEY 和 AUTH_SECRET
go test ./...
go run ./cmd/server
```

健康检查：

```bash
curl http://localhost:8080/healthz
```

启动时服务会建立 MySQL 连接并执行 Ping；如果数据库不可用，服务不会启动。

需求聊天接口已进入 Redis + Asynq 异步队列；Worker 会执行真实 LLM 需求采集 Agent、写入 Brief 新版本，并推进项目状态。方案生成会调用真实 LLM 方案 Agent 和图片 Provider，反馈后可生成二轮方案。

系统现在支持客户注册登录、项目列表和历史项目切换。项目数据按客户账号隔离。

需求聊天支持上传 `.docx` Word 文档，后端会提取正文和内嵌图片，并将图片保存为项目参考资产后交给需求采集 Agent。

## Docker 启动

配置 `.env` 后一键启动 MySQL、Redis 和 Go 服务：

```bash
cp .env.example .env
# 编辑 .env，填写真实 LLM_API_KEY，并生成 AUTH_SECRET
./scripts/start.sh
```

运行完整端到端测试时使用本地 Mock LLM：

```bash
./scripts/e2e.sh
```

E2E 测试默认使用宿主机 `18080` 端口（可通过 `APP_PORT` 覆盖），会创建项目、提交聊天任务、轮询 Asynq 任务状态，并确认 LLM 返回的 Brief 已写入 MySQL。

生产启动只使用真实 LLM 和图片 Provider；Mock LLM 仅由 `docker-compose.e2e.yml` 覆盖文件使用。真实 Word 上传、图片资产、二轮设计和故障排查步骤见：[生产与真实 LLM 操作手册](docs/OPERATIONS.md)。

完整的启动、MySQL 控制台、初始化、日志、重置和故障排查说明见：[本地运行操作手册](docs/OPERATIONS.md)。
