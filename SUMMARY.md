# 项目工作总结 — 第三方教务软件 Bug 反馈中心后端

> 本文档汇总整个后端项目的实现内容、关键设计决策、安全加固、验证状态与部署方式，作为交接与回顾的索引。详细使用说明见 [README.md](./README.md)。

## 1. 项目概述

为第三方教务软件（光序 / 光汇，Flutter 客户端）提供匿名 Bug 反馈服务：

- Flutter 客户端匿名提交 Bug 描述、联系方式和图片（JPG/PNG/WebP，≤5MB，最多 6 张）。
- 后端保存反馈内容与图片元数据，图片文件存腾讯云 COS **完全私有桶**。
- 管理员通过受保护接口查看反馈与图片（动态生成 Presign GET URL）。

**本期不做**：用户登录、历史反馈、状态流转、评论回复、消息通知、统计报表、图片审核、缩略图、EXIF 清理。

## 2. 技术栈

| 类别 | 选型 |
| --- | --- |
| 语言/运行 | Go 1.25+ 单体服务（中国大陆网络，`GOPROXY=https://goproxy.cn,direct`） |
| Web 框架 | `github.com/gin-gonic/gin` |
| 数据库 | PostgreSQL + `github.com/jackc/pgx/v5`（`pgxpool`，禁止 ORM） |
| 对象存储 | `github.com/tencentyun/cos-go-sdk-v5`（私有桶） |
| 限流 | `golang.org/x/time/rate`（进程内令牌桶） |
| 标准库 | `crypto/rand`+`encoding/base32`（ULID）、`embed`（迁移）、`os`（配置）、`log/slog`（JSON 日志）、`time`（定时任务） |
| 禁止引入 | ORM、Viper、Zap、Cron 库、Migrate 工具、第三方 ULID、Redis、MQ、Swagger |

## 3. 目录结构

```text
.
├── cmd/server/main.go          # 启动、路由、优雅退出、迁移执行
├── internal/
│   ├── config/config.go        # 环境变量读取（S3 风格）
│   ├── cos/client.go           # COS 封装（Presign/Head/GetRange/Delete）
│   ├── handler/                # Gin Handlers（admin/feedback/upload/health）+ response 统一响应
│   ├── middleware/             # auth/client_id/ratelimit/recovery/body_limit/logger
│   ├── model/model.go          # DB Models & DTOs
│   ├── repository/             # pgx/v5 数据访问层（全参数化 SQL）
│   ├── service/                # upload/feedback 业务 + ratelimit + orphan_cleaner
│   └── pkg/                    # idgen(ULID) / imagecheck(Magic Number) / apperr(业务错误)
├── migrations/schema.sql       # 表结构（embed 幂等执行）
├── migrations/embed.go         # go:embed 导出 schema
├── Dockerfile                  # 多阶段构建（非 root + HEALTHCHECK）
├── .dockerignore
├── .env.example                # 环境变量样例（S3 风格）
├── .gitignore
├── Makefile                    # GOPROXY/tidy/build/vet/run
├── go.mod / go.sum
└── README.md / SUMMARY.md
```

## 4. 接口与错误码

统一响应：成功 `{"code":0,"message":"ok","data":{}}`；失败 `{"code":错误码,"message":"错误信息"}`。

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/healthz` | 无 | 健康检查（独立于 `/api/v1`） |
| POST | `/api/v1/uploads/presign` | `X-Client-ID` | 获取上传凭证 |
| POST | `/api/v1/uploads/confirm` | `X-Client-ID` | 确认上传（Magic Number 校验） |
| POST | `/api/v1/feedbacks` | `X-Client-ID` | 提交反馈 |
| GET | `/api/v1/admin/feedbacks` | Bearer Token | 列表（分页/contact 模糊/app_name/时间） |
| GET | `/api/v1/admin/feedbacks/:feedback_no` | Bearer Token | 详情 + 图片 Presign GET URL |

错误码：`40001` 参数错误、`40002` 缺 X-Client-ID、`40101` 未授权、`40401` 资源不存在、`40901` 重复提交、`41301` 文件过大、`41501` 文件类型不支持、`42901` 请求过频（`Retry-After: 60`）、`50001` 服务内部错误。

## 5. 核心安全设计

1. **私有桶绝对安全**：COS 桶完全私有，禁止公开读写与列举，绝不暴露固定 URL；所有图片访问走后端实时生成的 Presign GET URL（默认 15 分钟）。
2. **防幽灵文件**：presign 阶段先插 `unconfirmed` DB 记录，再生成 PUT URL，确保 COS 每个文件都有 DB 记录兜底。
3. **Magic Number 二次校验**：confirm 阶段 `GetObjectRange(bytes=0-15)` 读文件头，严格校验 JPEG/PNG/WebP，失败标 `invalid`（不立即删，交孤儿清理）。
4. **体积约束（方案 B + D）**：COS 预签名不支持 `content-length-range` 区间，故把 `Content-Length` 钉死为声明的 size（≤5MB）纳入签名，客户端 PUT 字节数不符即 403；叠加网关 `client_max_body_size 5m` 兜底。
5. **幂等与归属**：`INSERT ... ON CONFLICT (request_id) DO NOTHING` 处理重复提交；冲突回查时校验 `client_id` 归属，仅同一 client 回显 `feedback_no`，避免跨 client 泄露。
6. **请求体限制**：`BodyLimit` 中间件（1MB，`http.MaxBytesReader`）防超大 JSON 导致 OOM。
7. **常数时间鉴权**：管理员 token 用 `crypto/subtle.ConstantTimeCompare` 比较。
8. **不落敏感日志**：SecretKey/Token 不打印；访问日志只记 path，不记 query/header；已核实 COS SDK 错误不含 Authorization 头。
9. **全参数化 SQL**：所有 DB 操作使用 `$1` 占位符，无拼接。
10. **admin 独立限流**：`/admin` 先过独立 IP 限流（`ADMIN_RATE_LIMIT_RPS`/`BURST`）再鉴权，防 token 爆破与误用。
11. **请求超时**：`HTTP_TIMEOUT_SECONDS`（默认 90s）+ `http.Server` Read/WriteTimeout，DB/COS 操作随请求 ctx 取消。
12. **健康检查探 DB**：`/healthz` 探测数据库，异常返回 503，LB/Docker HEALTHCHECK 可及时摘除异常节点。

## 6. 关键设计决策记录

在 PRD 与实现之间有过几处取舍，最终决策如下：

| 议题 | 决策 |
| --- | --- |
| confirm 阶段 file_key 不存在 | 返回 **40401**（对齐错误字典「file_key 不存在」），非 40001 |
| confirm 阶段 client_id 归属冲突 | 返回 **40001**（错误字典「归属冲突」） |
| COS 端点 | 采用**完整桶域名**（`S3_CDN_URL`，含桶名）；`S3_ENDPOINT`（不含桶名）仅服务级 API 用，对象操作不依赖 |
| 体积约束 | 方案 **B + D**（签名钉死 Content-Length + 网关兜底），不上 POST+Policy（复杂度高、非必要） |
| 配置命名 | 按生产环境采用 **S3 风格**（`STORAGE_PROVIDER`/`S3_*`），内部字段用 `Storage*` |
| 请求体大小限制 | 1MB 上限 |
| 孤儿清理 | 循环 `DeleteObject`，删除成功后才物理删 DB 记录，10 分钟一轮 |
| 日上限 | `created_at > now() - interval '24 hours'` 命中 `idx_feedback_client_time` |

## 7. 配置项（环境变量，S3 风格）

程序用标准库 `os.Getenv` 读**进程环境变量**，不自动读取 `.env` 文件（`.env.example` 仅为模板）。

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `APP_ENV` / `HTTP_ADDR` | 否 | `development` / `:8080` | 基础 |
| `DB_DSN` | **是** | - | 支持 `postgres://` 与 `postgresql://` |
| `STORAGE_PROVIDER` | 否 | `s3` | 固定标识 |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | **是** | - | COS SecretId/SecretKey |
| `S3_BUCKET` | **是** | - | 桶名（含 appid） |
| `S3_REGION` | 否 | `ap-guangzhou` | 地域 |
| `S3_CDN_URL` | 否 | 推导 | 桶访问域名（含桶名） |
| `S3_ENDPOINT` | 否 | - | 服务端点（对象操作不使用） |
| `S3_BASE_PREFIX` | 否 | 空 | 对象 key 前缀（如 `uploads`） |
| `PRESIGN_GET_EXPIRE_SECONDS` / `UPLOAD_PRESIGN_EXPIRE_SECONDS` | 否 | `900` | 预签名有效期 |
| `IMAGE_MAX_SIZE` / `IMAGE_MAX_COUNT` | 否 | `5242880` / `6` | 图片限制 |
| `CONTENT_MAX_LENGTH` / `CONTACT_MAX_LENGTH` | 否 | `2000` / `128` | 文本限制 |
| `ORPHAN_IMAGE_RETAIN_HOURS` | 否 | `24` | 孤儿保留时长 |
| `ADMIN_API_TOKEN` | **是** | - | 管理员 Bearer Token |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` / `FEEDBACK_DAILY_LIMIT` | 否 | `5` / `10` / `100` | 限流 |
| `HTTP_TIMEOUT_SECONDS` | 否 | `90` | 单请求超时（0 禁用） |
| `ADMIN_RATE_LIMIT_RPS` / `ADMIN_RATE_LIMIT_BURST` | 否 | `10` / `30` | admin 独立限流 |
| `CORS_ALLOWED_ORIGINS` | 否 | 空 | 可选 CORS 白名单 |

## 8. 验证状态

| 检查项 | 结果 |
| --- | --- |
| `go build ./...` | ✅ 通过 |
| `go vet ./...` | ✅ 通过 |
| 单元测试 | ✅ 2026-08-16 补充：`go test ./...` 覆盖 `imagecheck`(100%)/`apperr`(100%)/`response`(100%)/`idgen`(91.7%)/`config`(81.7%)/`ratelimit`/`client_id`/`auth`，含竞态检测（`make test` / `make test-race`） |
| `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` 交叉编译（= Docker 构建阶段） | ✅ 通过，产出 ~25MB 静态二进制 |
| 真实 COS / PostgreSQL 联调 | ✅ 2026-08-16 本机执行：Windows 直跑二进制 + 本地 PG18 测试库 `bug_feedback_e2e` + 真实 COS，`scripts/e2e_smoke.py` **12/12 通过**（presign/PUT/confirm/Magic Number/幂等/admin/图片下载）。远程库 `43.128.23.102:25432` 本机无法完成 PG 握手（TCP 通、协议层无响应，疑似安全组白名单），生产联调需在部署机或放行后执行 |
| `docker build` | ⏳ 未执行（本机未安装 Docker） |

## 9. 部署方式

- **本地 DB**：`docker run -d ... postgres:16-alpine`（见 README 快速开始）。
- **容器化**：`docker build -t bug-feedback-backend .`（多阶段、非 root、内置 HEALTHCHECK、`GOPROXY=goproxy.cn`）；运行用 `docker run -e ... ` 注入环境变量（见 README「Docker 构建与运行」）。
- **优雅退出**：HTTP Server 与孤儿清理任务均随 `SIGINT/SIGTERM` 优雅停止。

## 10. 已知边界与后续演进（非本期范围）

- 匿名 `client_id` 可伪造（匿名身份设计固有属性）；`IP+client_id` 复合限流可被轮换 client_id 部分绕过，后续可加纯 IP 独立限流。
- Magic Number 仅校验文件头，非完整图片解码（PRD 明确「图片审核不做」）。
- 多实例部署：迁移执行与孤儿清理已通过 advisory lock 互斥；进程内限流仍为单实例语义（多副本总限流按副本数放大），需引入 Redis/网关层解决；CDN 加速需鉴权 URL；图片审核/EXIF 清理需独立服务。

## 11. 与 PRD 的对齐说明

最终实现严格覆盖 PRD 的功能与安全要求，并在以下几处做了显式取舍（详见第 6 节）：错误码归属、COS 端点格式、体积约束方案、配置命名风格。SDK 侧约定（`size == imageBytes.length`、`request_id` 幂等、`client_id` 持久化）已写入 README「Flutter 客户端对接流程」。
