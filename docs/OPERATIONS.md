# Packaging AI 本地运行操作手册

本文档适用于当前 Docker Compose 版本，包含 MySQL、Redis、Go API 服务和 Asynq Worker。

## 1. 前置条件

需要安装并启动：

- Docker Desktop
- Docker Compose v2
- Go（运行本机测试时需要）
- `curl`（运行 E2E 脚本时需要）

确认 Docker daemon 可用：

```bash
docker info
docker compose version
```

如果出现 Docker socket 权限错误，先启动 Docker Desktop；在 Codex 终端中需要使用允许访问 Docker Desktop socket 的授权终端执行 Docker 命令。

## 2. 服务组成

| 服务 | Compose 服务名 | 容器端口 | 本机访问 |
|---|---|---:|---|
| MySQL | `mysql` | 3306 | `127.0.0.1:3306` |
| Redis | `redis` | 6379 | `127.0.0.1:6379` |
| Go API/Worker | `app` | 8080 | `127.0.0.1:8080` |
| E2E Mock LLM | `mock-llm` | 8080 | 仅 Compose 内部访问 |

Go 服务在容器网络中使用 `mysql:3306` 和 `redis:6379`，不能把 `localhost` 写进容器内的数据库或 Redis 地址。

## 3. 首次启动

### 3.1 启动真实 LLM 模式

生产 Compose 不再启动 Mock LLM。Go 服务会直接调用 `.env` 中配置的 OpenAI 兼容 LLM 和图片生成 Provider。

```bash
cp .env.example .env
openssl rand -hex 32
```

把生成的随机值写入 `.env` 的 `AUTH_SECRET`，再填写真实的 `LLM_API_KEY`。之后启动不需要重复设置环境变量。

启动全部服务：

```bash
./scripts/start.sh
```

默认 API 地址：

```text
http://localhost:8080
```

如果 8080 已被占用，可以指定本机端口：

```bash
APP_PORT=18080 ./scripts/start.sh
```

### 3.2 启动并运行完整 E2E

E2E 使用本地 Mock LLM，不需要真实 API Key：

```bash
./scripts/e2e.sh
```

E2E 默认使用本机 `18080` 端口，也可以指定：

```bash
APP_PORT=19080 ./scripts/e2e.sh
```

测试流程包括：创建项目、提交聊天任务、Redis/Asynq 调度、Mock LLM 调用、Brief 写入 MySQL、任务状态轮询和结果校验。

测试结束后，脚本会删除容器和网络，但默认保留 MySQL 和 Redis 数据卷。

生产模式和 E2E 模式是两个不同的 Compose 组合：

```bash
# 生产：只启动 MySQL、Redis 和 Go 服务，调用真实 Provider
docker compose up -d --build

# E2E：额外覆盖为 Mock LLM，不消耗真实额度
./scripts/e2e.sh
```

如果没有设置 `LLM_API_KEY` 或 `AUTH_SECRET`，生产 Compose 会在解析配置时直接停止，避免带着假密钥启动。

## 4. 查看服务状态

```bash
docker compose ps
```

查看容器：

```bash
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
```

健康检查：

```bash
curl http://localhost:8080/healthz
```

如果使用其他端口，将 `8080` 替换为对应的 `APP_PORT`。

## 5. 查看日志

查看 Go 服务：

```bash
docker compose logs -f app
```

查看 MySQL：

```bash
docker compose logs -f mysql
```

查看 Redis：

```bash
docker compose logs -f redis
```

查看最近 100 行：

```bash
docker compose logs --tail=100 app mysql redis
```

## 6. MySQL 数据库初始化

Compose 会在 MySQL 数据目录为空时自动执行：

```text
migrations/001_init.sql
```

数据库配置：

```text
数据库名：packaging_ai
普通用户：packaging
普通密码：packaging
root 密码：root
```

初始化脚本只会在第一次创建 MySQL 数据目录时执行。如果 MySQL volume 已经存在，修改迁移文件不会自动重新执行。

## 7. 访问 MySQL 控制台

### 7.1 从 MySQL 容器内进入

```bash
docker compose exec mysql mysql -u packaging -p packaging_ai
```

密码：

```text
packaging
```

使用 root：

```bash
docker compose exec mysql mysql -u root -p packaging_ai
```

密码：

```text
root
```

### 7.2 使用本机 MySQL 客户端

本机安装了 `mysql` 命令时：

```bash
mysql -h 127.0.0.1 -P 3306 -u packaging -p packaging_ai
```

### 7.3 常用 SQL

```sql
SHOW TABLES;

SELECT id, name, status, current_round
FROM projects
ORDER BY created_at DESC;

SELECT project_id, version, completion_score, updated_at
FROM briefs
ORDER BY updated_at DESC;

SELECT project_id, agent_type, status, model, created_at, completed_at
FROM agent_runs
ORDER BY created_at DESC
LIMIT 20;
```

退出控制台：

```sql
exit;
```

## 8. 数据重置

仅停止容器，保留数据：

```bash
docker compose down
```

停止容器并删除 MySQL、Redis 数据卷：

```bash
docker compose down -v
```

下一次启动时会重新创建数据库并执行 `migrations/001_init.sql`。此操作会删除本地项目、Brief、Agent Run 和 Redis 队列数据。

## 9. 本机代码检查

```bash
GOCACHE=/tmp/packaging-ai-go-cache go test ./...
GOCACHE=/tmp/packaging-ai-go-cache go vet ./...
```

E2E 测试不直接执行在普通 `go test ./...` 中，必须先启动 Docker Compose，或直接运行：

```bash
./scripts/e2e.sh
```

## 10. 常见问题

### 8080 端口被占用

检查占用进程：

```bash
lsof -nP -iTCP:8080 -sTCP:LISTEN
```

换用其他端口：

```bash
APP_PORT=18080 ./scripts/start.sh
```

### MySQL 容器健康检查失败

查看日志：

```bash
docker compose logs mysql
```

如果是旧数据卷与当前初始化脚本不一致，确认数据可以删除后执行：

```bash
docker compose down -v
./scripts/start.sh
```

### Go 服务无法连接 MySQL 或 Redis

容器内配置必须使用服务名：

```text
mysql:3306
redis:6379
```

不要在 Compose 内把它们配置成 `127.0.0.1` 或 `localhost`。

### E2E 任务失败

查看四个服务的日志：

```bash
docker compose -f docker-compose.yml -f docker-compose.e2e.yml logs --tail=200 app mysql redis mock-llm
```

确认 Mock LLM 健康：

```bash
docker compose -f docker-compose.yml -f docker-compose.e2e.yml ps
```
