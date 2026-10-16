# 配置说明

服务通过环境变量配置：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `HTTP_ADDR` | `:8080` | HTTP 监听地址 |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/packaging_ai?parseTime=true&charset=utf8mb4&loc=Local` | MySQL 连接串 |
| `MYSQL_MAX_OPEN_CONNS` | `20` | 最大打开连接数 |
| `MYSQL_MAX_IDLE_CONNS` | `10` | 最大空闲连接数 |
| `MYSQL_CONN_MAX_LIFETIME_MIN` | `30` | 连接最大生命周期（分钟） |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis 地址 |
| `REDIS_PASSWORD` | 空 | Redis 密码 |
| `REDIS_DB` | `0` | Redis 数据库编号 |
| `REDIS_QUEUE` | `packaging-ai` | Asynq 队列名称 |
| `REDIS_CONCURRENCY` | `4` | Worker 并发数 |
| `LLM_BASE_URL` | `https://api.openai.com/v1` | OpenAI 兼容 API 地址 |
| `LLM_API_KEY` | 无 | LLM API 密钥，启动必填 |
| `LLM_MODEL` | `gpt-4o-mini` | 需求采集模型 |
| `IMAGE_BASE_URL` | 使用 `LLM_BASE_URL` | 图片生成 API 地址 |
| `IMAGE_API_KEY` | 使用 `LLM_API_KEY` | 图片生成 API 密钥 |
| `IMAGE_MODEL` | `gpt-image-1` | 图片生成模型；以 Provider `/models` 返回的可用模型为准，当前验证使用 `gpt-image-1.5` |
| `AUTH_SECRET` | 本地开发默认值 | HMAC Token 签名密钥，生产环境必须替换 |
| `AUTH_TTL_HOURS` | `24` | 登录 Token 有效期（小时） |

初始化数据库：

```sql
CREATE DATABASE packaging_ai
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;
```

然后执行 `migrations/001_init.sql`。

DSN 至少应包含：

```text
parseTime=true&charset=utf8mb4
```

这样 MySQL 的 `DATETIME(3)` 才能正确扫描为 Go 的 `time.Time`。

启动前需要 Redis 可连接，并在项目根目录 `.env` 中设置真实 LLM 和图片 Provider。应用会自动加载这个文件：

```bash
cp .env.example .env
# 编辑 .env，填写 LLM_API_KEY、IMAGE_API_KEY、AUTH_SECRET
./scripts/start.sh
```

Docker Compose 使用 `mysql` 和 `redis` 作为服务名；`docker-compose.e2e.yml` 会覆盖 LLM 地址，使用本地 Mock LLM，不消耗真实模型额度。
