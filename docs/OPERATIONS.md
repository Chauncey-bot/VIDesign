# Packaging AI 生产与真实 LLM 操作手册

本文档说明如何使用本地 `.env` 配置启动 MySQL、Redis、Go API/Asynq Worker，并通过前端上传 Word 需求，完成需求采集、方案生成、客户反馈和二轮设计。当前服务使用 OpenAI 兼容接口；生产环境不会启动 Mock LLM。

## 1. 运行前检查

需要 Docker Desktop、Docker Compose v2、`curl`、`jq`。本机执行 Go 检查还需要 Go。

```bash
docker info
docker compose version
```

如果 Docker socket 报权限错误，先启动 Docker Desktop，再在有权访问 Docker Desktop 的终端执行命令。

服务端口：

| 服务 | 容器端口 | 默认本机端口 |
|---|---:|---:|
| MySQL | 3306 | 3306 |
| Redis | 6379 | 6379 |
| Go API/Asynq Worker | 8080 | 8080 |

容器之间必须使用 `mysql:3306` 和 `redis:6379`，不能使用容器内的 `localhost`。

## 2. 配置本地 `.env`

第一次使用：

```bash
cp .env.example .env
openssl rand -hex 32
```

编辑 `.env`，至少填写：

```dotenv
LLM_BASE_URL=https://your-openai-compatible-host/v1
LLM_API_KEY=replace-with-real-api-key
LLM_MODEL=gpt-6.1-sol
IMAGE_BASE_URL=https://your-openai-compatible-host/v1
IMAGE_API_KEY=replace-with-real-api-key
IMAGE_MODEL=gpt-image-1.5
AUTH_SECRET=replace-with-a-long-random-secret
```

不要把真实 Key 写入 Git、截图、日志或 `.env.example`。当前实现会自动读取项目根目录的 `.env`，启动命令不需要重复 `export`。

启动前检查配置是否已填入，但不要打印 Key：

```bash
awk -F= '/^(LLM_BASE_URL|LLM_MODEL|LLM_API_KEY|IMAGE_BASE_URL|IMAGE_MODEL|IMAGE_API_KEY|AUTH_SECRET)=/ { \
  if ($1 ~ /API_KEY|AUTH_SECRET/) print $1"="(length($2)>0?"configured":"empty"); else print $0 \
}' .env
```

如果 Provider 的图片模型不同，先查询模型列表：

```bash
base=$(awk -F= '$1=="LLM_BASE_URL"{print substr($0,index($0,"=")+1)}' .env)
key=$(awk -F= '$1=="LLM_API_KEY"{print substr($0,index($0,"=")+1)}' .env)
curl -fsS "$base/models" -H "Authorization: Bearer $key" | jq -r '.data[]?.id'
```

把实际支持的图片模型写入 `IMAGE_MODEL`。例如当前验证使用 `gpt-image-1.5`；`gpt-image-1` 在该代理账户组中不可用。

## 3. 生产启动

```bash
./scripts/start.sh
```

脚本会读取 `.env`，构建并启动 MySQL、Redis 和 Go 服务。检查状态：

```bash
docker compose ps
curl -fsS http://localhost:8080/healthz
```

如果 8080 已被占用：

```bash
APP_PORT=18080 ./scripts/start.sh
curl -fsS http://localhost:18080/healthz
```

浏览器打开：

```text
http://localhost:8080
```

或使用实际的 `APP_PORT`。

生产 Compose 只启动真实 Provider；不要使用 `docker-compose.e2e.yml` 覆盖文件，否则会切换到 Mock LLM。

## 4. 数据库初始化与升级

MySQL 第一次创建空数据卷时，会执行：

```text
migrations/001_init.sql
migrations/002_auth.sql
```

默认数据库：`packaging_ai`；Compose 默认账号为 `packaging/packaging`，root 密码为 `root`。已有数据卷不会重新执行 init 脚本，但 Go 服务启动时会执行兼容性检查，并将 `assets.storage_url` 调整为 `LONGTEXT`，以保存图片 Base64 数据 URL。

进入 MySQL：

```bash
docker compose exec mysql mysql -u packaging -p packaging_ai
# 密码：packaging
```

常用检查：

```sql
SHOW TABLES;
SELECT id, name, status, current_round FROM projects ORDER BY created_at DESC;
SELECT project_id, version, completion_score FROM briefs ORDER BY updated_at DESC LIMIT 20;
SELECT project_id, agent_type, status, model, created_at, completed_at
FROM agent_runs ORDER BY created_at DESC LIMIT 20;
SELECT id, asset_type, model, LENGTH(storage_url) AS bytes
FROM assets ORDER BY created_at DESC LIMIT 20;
```

退出：

```sql
exit;
```

确认数据可以删除时，才执行完整重置：

```bash
docker compose down -v
./scripts/start.sh
```

这会删除本地 MySQL 和 Redis 数据卷。

## 5. 前端上传 Word 并完成真实设计流程

### 5.1 前端操作

1. 打开前端并注册或登录。
2. 创建项目。
3. 在“需求采集”中按四步向导填写：产品与品牌、客户与定位、包装与材质、文字与参考。
4. 在第一步上传 `.docx`，正文和内嵌图片会随本步提交给需求采集 Agent。
5. 每一步提交后等待任务完成；四步完成后查看折叠的“设计 Brief”。
6. Brief 完整度达到 80% 以上后，在“方案与反馈”点击生成 9 套第一轮方案。
7. 勾选方案后可批量下载图片；选择一套填写评分和修改意见，提交“反馈并生成二轮”。
8. 等待二轮图片生成完成，检查方案图片、说明和 PDF 报告。

Word 解析内容包括：

- `word/document.xml` 中的正文文本；
- `word/media/*` 中的内嵌 PNG/JPG/GIF/WebP 图片；
- 图片会保存为项目参考资产，并以多模态图片输入传给需求采集 Agent。

如果 Chrome 文件选择器提示需要允许访问文件网址：

1. 手动打开 `chrome://extensions`；
2. 找到 ChatGPT/Codex 扩展；
3. 打开“详细信息”；
4. 开启“允许访问文件网址”；
5. 返回前端并刷新页面。

### 5.2 用 API 验证 Word 正文和图片

当需要排查前端之外的后端链路时，可以用 API 上传。先登录并取得 Token，再创建项目：

```bash
BASE=http://localhost:8080
TOKEN='登录返回的 token'
PROJECT_ID=$(curl -fsS -X POST "$BASE/api/v1/projects" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"name":"Word 需求验证"}' | jq -r .id)
```

上传 Word：

```bash
curl -fsS -X POST "$BASE/api/v1/projects/$PROJECT_ID/chat" \
  -H "Authorization: Bearer $TOKEN" \
  -F 'message=请完整读取 Word 需求并提取产品、客群、风格、材质和图片参考' \
  -F 'document=@/path/to/需求文档.docx;type=application/vnd.openxmlformats-officedocument.wordprocessingml.document'
```

返回 `task_id` 后轮询：

```bash
TASK_ID='返回的 task_id'
while true; do
  result=$(curl -fsS "$BASE/api/v1/tasks/$TASK_ID" -H "Authorization: Bearer $TOKEN")
  echo "$result" | jq -c '{status,error,payload}'
  state=$(echo "$result" | jq -r .status)
  [ "$state" = succeeded ] && break
  [ "$state" = failed ] && exit 1
  sleep 2
done
```

成功时重点检查：

```json
{
  "payload": {
    "document_text": "Word 正文内容",
    "document_asset_ids": ["内嵌图片资产 ID"]
  }
}
```

读取 Brief 和图片资产：

```bash
curl -fsS "$BASE/api/v1/projects/$PROJECT_ID/brief" \
  -H "Authorization: Bearer $TOKEN" | jq .

curl -fsS -o reference-image.bin \
  "$BASE/api/v1/assets/$ASSET_ID" \
  -H "Authorization: Bearer $TOKEN"
file reference-image.bin
```

### 5.3 生成第一轮方案

```bash
curl -fsS -X POST "$BASE/api/v1/projects/$PROJECT_ID/generate" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"proposal_count":9}'
```

生成过程会依次调用真实 LLM 方案 Agent 和图片 Provider。图片生成可能需要几十秒，不能只看到 `queued` 就判定失败。完成后检查：

```bash
curl -fsS "$BASE/api/v1/projects/$PROJECT_ID/proposals" \
  -H "Authorization: Bearer $TOKEN" | jq 'map({id,round,title,assets:(.assets|length)})'
```

前端默认生成 9 套第一轮方案。方案卡片支持勾选，点击“批量下载图片”会按选择逐张下载；如果浏览器提示允许多个下载，请选择允许。

### 5.4 提交反馈并生成二轮

```bash
PROPOSAL_ID='第一轮方案 ID'

curl -fsS -X POST "$BASE/api/v1/proposals/$PROPOSAL_ID/feedback" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"rating":4,"comment":"保留主色，减少插画复杂度并突出核心卖点"}'

curl -fsS -X POST "$BASE/api/v1/projects/$PROJECT_ID/revision" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  --data "{\"selected_proposal_id\":\"$PROPOSAL_ID\",\"feedback\":{\"rating\":4,\"comment\":\"保留主色，减少插画复杂度并突出核心卖点\"},\"preserve\":[\"主色\"]}"
```

检查二轮结果：

```bash
curl -fsS "$BASE/api/v1/projects/$PROJECT_ID/proposals?round=2" \
  -H "Authorization: Bearer $TOKEN" | jq 'map({id,round,title,assets:(.assets|length)})'
```

导出 PDF：

```bash
curl -fsS -X POST "$BASE/api/v1/projects/$PROJECT_ID/export" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"formats":["pdf","markdown"]}'
curl -fsS -o packaging-report.pdf \
  "$BASE/api/v1/projects/$PROJECT_ID/export?format=pdf" \
  -H "Authorization: Bearer $TOKEN"
file packaging-report.pdf
```

## 6. E2E 验证模式

E2E 使用本地 Mock LLM，不消耗真实 Provider 额度：

```bash
./scripts/e2e.sh
```

指定端口：

```bash
APP_PORT=19080 ./scripts/e2e.sh
```

查看 E2E 日志：

```bash
docker compose -f docker-compose.yml -f docker-compose.e2e.yml logs --tail=200 app mysql redis mock-llm
```

本机代码检查：

```bash
GOCACHE=/tmp/packaging-ai-go-cache go test ./...
GOCACHE=/tmp/packaging-ai-go-cache go vet ./...
```

## 7. 故障排查

### 7.1 配置缺失

错误：`LLM_BASE_URL, LLM_API_KEY and LLM_MODEL are required` 或 Compose 提示 `LLM_API_KEY must be set`。

处理：确认根目录存在 `.env`，且 `LLM_API_KEY`、`IMAGE_API_KEY`、`AUTH_SECRET` 非空；修改后执行：

```bash
APP_PORT=18080 docker compose up -d --build
```

### 7.2 `model_not_found` 或图片模型 404

错误示例：`Model "gpt-image-1" is not supported by any configured account in this group`。

处理：调用 `/models` 查询当前 Provider 支持的模型，把有效模型写入 `IMAGE_MODEL`，然后重建 `app`。当前验证使用 `gpt-image-1.5`。

### 7.3 Brief 第二轮采集出现 MySQL 1062

错误：`Duplicate entry ... for key 'briefs.PRIMARY'`。

当前代码已为每个 Brief 版本生成新的 ID，并在启动时执行兼容检查。确认运行的是最新镜像：

```bash
APP_PORT=18080 docker compose up -d --build app
```

### 7.4 图片 Base64 导致 MySQL 1406

错误：`Data too long for column 'storage_url'`。

当前迁移和启动兼容逻辑已将 `assets.storage_url` 改为 `LONGTEXT`。检查：

```bash
docker compose exec mysql mysql -u packaging -ppackaging -N -e \
  'USE packaging_ai; SHOW COLUMNS FROM assets LIKE "storage_url";'
```

### 7.5 二轮 Agent 返回上下文超限 502

如果修订请求把第一轮图片 Base64 一起发送，会出现 `input exceeds the context window`。当前代码会在调用修订 Agent 前移除图片资产，只保留方案文本、颜色、策略和反馈。重建最新 `app` 后重新提交二轮任务。

### 7.6 Asynq 任务长时间 queued/running

先查看状态和日志：

```bash
curl -fsS "$BASE/api/v1/tasks/$TASK_ID" -H "Authorization: Bearer $TOKEN" | jq .
docker compose logs --tail=200 app redis
```

真实图片生成可能持续几十秒。若任务明确失败，先检查 `error` 字段和 Provider 返回；不要反复点击生成造成重复方案。测试任务清理前应先确认项目 ID，生产数据不要直接删除 Redis 键。

### 7.7 8080 端口冲突

```bash
lsof -nP -iTCP:8080 -sTCP:LISTEN
APP_PORT=18080 ./scripts/start.sh
```

### 7.8 MySQL 或 Redis 不健康

```bash
docker compose ps
docker compose logs --tail=200 mysql redis
```

确认 Docker 内连接地址为 `mysql:3306`、`redis:6379`。只有在宿主机直接运行 Go 服务时，才使用 `127.0.0.1`。

### 7.9 浏览器无法选择本地 Word

在 Chrome 扩展详情中开启“允许访问文件网址”，刷新前端。如果暂时不能修改浏览器策略，可先用上面的 multipart API 验证后端 Word 解析和图片资产链路。

## 8. 停止服务

保留数据：

```bash
docker compose down
```

删除数据卷（不可逆地删除本地数据）：

```bash
docker compose down -v
```
