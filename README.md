# Cloud Clip Lite

轻量级云剪切板 Web 应用 — 自托管、跨平台、iPhone 快捷指令友好。

## 功能特性

- **剪切板同步**：文本、图片、文件，多端实时同步（WebSocket）
- **账号体系**：Argon2id 密码哈希、JWT + API Token 双凭证、三种注册模式
- **存储管理**：单条上限、总配额、TTL 自动过期、配额超额自动清理
- **管理后台**：用户管理、系统统计、审计日志、Prometheus 指标
- **安全加固**：MIME magic bytes 校验、令牌桶限流、XSS 防护、CORS 控制
- **生产就绪**：Docker 一键部署、优雅关闭、结构化日志、健康检查

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.24 + chi + slog + database/sql |
| 数据库 | SQLite（默认）/ PostgreSQL |
| 前端 | React 19 + TypeScript + Tailwind CSS v4 + Vite |
| 实时 | WebSocket（gorilla/websocket） |
| 部署 | Docker + docker-compose + Caddy（可选 TLS） |

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
# 1. 克隆仓库
git clone https://github.com/leaf/cloud-clip-lite.git
cd cloud-clip-lite

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，至少修改 JWT_SECRET 和 ADMIN_PASSWORD

# 3. 启动
docker compose up -d

# 4. 访问
# http://localhost:8080
```

### 方式二：本地开发

```bash
# 后端
export JWT_SECRET="dev-secret-at-least-32-bytes-long!!!"
export ALLOW_REGISTER=open
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD=admin123
go run ./cmd/server/

# 前端（另一个终端）
cd web
npm install
npm run dev
# 访问 http://localhost:3000
```

### 方式三：二进制直接运行

```bash
# 构建前端
cd web && npm ci && npm run build && cd ..

# 构建后端
go build -o bin/server ./cmd/server/

# 运行
export JWT_SECRET="your-secret-at-least-32-bytes"
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD=your-password
./bin/server
```

## 配置说明

所有配置通过环境变量加载，参见 `.env.example` 获取完整列表。

### 必填项

| 变量 | 说明 | 示例 |
|---|---|---|
| `JWT_SECRET` | JWT 签名密钥，≥32 字节 | `openssl rand -base64 64` |
| `ADMIN_USERNAME` | 管理员用户名（首次启动） | `admin` |
| `ADMIN_PASSWORD` | 管理员密码（首次启动） | `strong-password-123` |

### 常用配置

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORT` | `8080` | 监听端口 |
| `DATABASE_URL` | 空（SQLite） | PostgreSQL 连接串 |
| `SQLITE_PATH` | `./data/clipboard.db` | SQLite 文件路径 |
| `BLOB_STORE` | `local` | 对象存储类型 |
| `ALLOW_REGISTER` | `closed` | 注册模式：closed/invite/open |
| `ALLOWED_ORIGINS` | `*` | CORS 源（生产环境填实际域名） |
| `RATE_LIMIT_PER_MINUTE` | `60` | 每用户每分钟请求上限 |
| `LOG_LEVEL` | `info` | 日志级别 |
| `LOG_FORMAT` | `json` | 日志格式 |

## API 端点

### 认证
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/register` | 注册 |
| POST | `/api/auth/login` | 登录 |
| POST | `/api/auth/refresh` | 刷新 Token |
| POST | `/api/auth/logout` | 退出 |

### 剪切板
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/clip` | 上传（文本 JSON / 文件 multipart） |
| GET | `/api/clip` | 列表（游标分页 + 类型过滤） |
| GET | `/api/clip/:id` | 详情 |
| GET | `/api/clip/:id/content` | 下载内容 |
| GET | `/api/clip/latest` | 最新条目 |
| DELETE | `/api/clip/:id` | 删除 |

### 设备管理
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/devices` | 列表 |
| POST | `/api/devices` | 创建（返回 API Token） |
| DELETE | `/api/devices/:id` | 删除 |
| POST | `/api/devices/:id/revoke` | 吊销 Token |

### 管理后台（需 admin 角色）
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/stats` | 系统统计 |
| GET | `/api/admin/users` | 用户列表 |
| PATCH | `/api/admin/users/:id` | 更新用户 |
| POST | `/api/admin/users/:id/reset-password` | 重置密码 |
| POST | `/api/admin/users/:id/force-logout` | 强制下线 |
| DELETE | `/api/admin/users/:id` | 删除用户 |
| GET | `/api/admin/audit-logs` | 审计日志 |

### 其他
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/healthz` | 存活检查 |
| GET | `/readyz` | 就绪检查 |
| GET | `/metrics` | Prometheus 指标 |
| WS | `/api/ws` | WebSocket 实时同步 |

## iPhone 快捷指令

参见 [iPhone 快捷指令配置指南](docs/iphone-shortcuts.md)。

## 部署指南

### Docker + Caddy（自动 HTTPS）

1. 配置域名 DNS 指向服务器
2. 编辑 `Caddyfile.example`，替换 `clip.example.com` 为你的域名
3. 取消 `docker-compose.yml` 中 Caddy 服务的注释
4. `docker compose up -d`

### Fly.io

```bash
fly launch --no-deploy
# 编辑 fly.toml 设置环境变量
fly secrets set JWT_SECRET=$(openssl rand -base64 64)
fly secrets set ADMIN_PASSWORD=your-password
fly deploy
```

### VPS（二进制 + systemd）

```bash
# 构建二进制
CGO_ENABLED=0 go build -o /usr/local/bin/cloud-clip ./cmd/server/

# 创建 systemd 服务
cat > /etc/systemd/system/cloud-clip.service << 'EOF'
[Unit]
Description=Cloud Clip Lite
After=network.target

[Service]
Type=simple
User=cloud-clip
EnvironmentFile=/etc/cloud-clip/.env
ExecStart=/usr/local/bin/cloud-clip
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl enable --now cloud-clip
```

## 项目结构

```
cloud-clip-lite/
├── cmd/server/           # 主入口
├── internal/
│   ├── api/              # HTTP 处理器
│   ├── auth/             # 密码哈希 + JWT + Token
│   ├── blob/             # 对象存储 + MIME 校验
│   ├── config/           # 环境变量配置
│   ├── db/               # 数据库抽象层
│   ├── health/           # 健康检查
│   ├── logger/           # 结构化日志
│   ├── metrics/          # Prometheus 指标
│   ├── middleware/        # 中间件（鉴权/限流/CORS 等）
│   ├── migrate/          # 数据库迁移
│   ├── scheduler/        # 定时任务（TTL 清理/配额/孤儿回收）
│   ├── store/            # 数据访问层
│   └── ws/               # WebSocket 实时同步
├── web/                  # React 前端
├── clients/              # 客户端占位（desktop/mobile/cli）
├── docs/                 # 文档
├── Dockerfile            # 多阶段构建
├── docker-compose.yml    # 编排文件
└── Caddyfile.example     # Caddy 反代配置
```

## 开发

```bash
# 运行全部测试
go test ./... -count=1

# 前端类型检查
cd web && npx tsc --noEmit

# 前端构建
cd web && npm run build
```

## License

MIT
