# 第三方教务软件 Bug 反馈中心后端

极简、高安全的匿名 Bug 反馈服务：Flutter 客户端（光序 / 光汇）匿名提交 Bug 描述、联系方式和图片；后端保存反馈内容与图片元数据；管理员通过受保护的接口管理反馈中心、生成校验码并查看反馈与图片。

**本期不做**：用户登录、用户历史反馈、反馈状态流转、评论回复、消息通知、统计报表、图片审核、缩略图、EXIF 清理。

## 目录结构

```text
.
├── cmd/server/main.go          # 启动、路由、优雅退出
├── internal/
│   ├── config/config.go        # 环境变量读取
│   ├── cos/client.go           # 腾讯云 COS 封装（Presign/Head/GetRange/Delete）
│   ├── feedbackcache/          # Redis 缓存、校验码、防重放、限流与分布式锁
│   ├── handler/                # Gin Handlers (admin, feedback, upload, response)
│   ├── middleware/             # auth, client_id, feedback_code, ratelimit, request_id, logger
│   ├── model/model.go          # DB Models & DTOs
│   ├── repository/             # pgx/v5 数据访问层
│   ├── service/                # 业务逻辑（含 orphan_cleaner、ratelimit）
│   └── pkg/                    # feedbackcode, secretbox, idgen, imagecheck, apperr
├── migrations/schema.sql       # embed 执行
├── Makefile                    # 构建与代理配置
└── .env.example                # 环境变量样例
```

## 技术栈

- Go 1.25+ 单体服务（中国大陆网络环境，`Makefile` 已配置 `GOPROXY=https://goproxy.cn,direct`）
- Web 框架：`gin`
- 数据库：PostgreSQL 16 + `pgx/v5`（`pgxpool` 连接池，禁止 ORM）
- 缓存与安全：Redis 7+ + `go-redis/v9`（校验码一次性消费、nonce 防重放、限流）
- 对象存储：腾讯云 COS（`cos-go-sdk-v5`，完全私有桶）
- 限流：`golang.org/x/time/rate`（进程内令牌桶）
- 标准库：`crypto/rand` + `encoding/base32`（ULID）、`embed`（迁移）、`os`（配置）、`log/slog`（JSON 日志）、`time`（定时任务）

## 快速开始

```bash
# 1. 启动本地 PostgreSQL 16 与 Redis 7
docker compose up -d

# 查看容器与健康状态
docker compose ps

# 2. 配置环境变量（参考 .env.example）
export DB_DSN='postgres://bug:bug@localhost:5432/bug_feedback?sslmode=disable'
export REDIS_ADDR='127.0.0.1:6379'
export FEEDBACK_SECRET_KEY='replace-with-a-long-random-secret'
export S3_BUCKET='your-bucket-1250000000'
export S3_ACCESS_KEY='your-secret-id'
export S3_SECRET_KEY='your-secret-key'
export ADMIN_API_TOKEN='change-me-to-a-long-random-token'

# 3. 整理依赖并运行
make tidy
make run
```

启动后 `GET /healthz` 返回 `{"code":0,"message":"ok","data":{}}`，并在启动时幂等执行 `migrations/schema.sql` 建表（无需第三方迁移工具）。

停止本地依赖：

```bash
docker compose down
```

如需同时删除 PostgreSQL 与 Redis 数据卷，使用 `docker compose down -v`。

### Windows 原生运行（不使用 Docker）

适用于 Docker Desktop 不可用，或只需要直接启动 PostgreSQL、Redis 和 Go 服务的本地调试环境。

需要安装：

- Go 1.25+
- PostgreSQL 16（当前代码也已验证兼容 PostgreSQL 10.17，但正式开发建议使用 16）
- Redis 7+。不要使用 Redis 3.x，多字段 `HSET` 会导致反馈中心缓存 `EXECABORT`
- PowerShell
- 腾讯云 COS 配置。只调试反馈中心管理接口时可以填占位值；图片上传与确认必须使用真实 COS 凭据

先初始化一套隔离的运行目录。下面的命令不会修改 Docker 数据，也不会把数据写入仓库：

```powershell
$runtime = Join-Path $env:USERPROFILE ".lens-backend"
New-Item -ItemType Directory -Force `
  "$runtime\pgdata", "$runtime\redis", "$runtime\logs"

# 初始化 PostgreSQL。使用 trust 仅用于本机开发。
initdb -D "$runtime\pgdata" `
  -U bug -A trust --encoding=UTF8 --locale=C

# 启动 PostgreSQL 并创建数据库
pg_ctl -D "$runtime\pgdata" `
  -l "$runtime\logs\postgres.log" `
  -o "-p 5432" -w start
createdb -h 127.0.0.1 -p 5432 -U bug bug_feedback
```

在另一个 PowerShell 窗口启动 Redis：

```powershell
$runtime = Join-Path $env:USERPROFILE ".lens-backend"
redis-server `
  --port 6379 `
  --bind 127.0.0.1 `
  --dir "$runtime\redis" `
  --appendonly no
```

配置后端所需的最小环境变量。COS 占位值只用于服务启动和反馈中心调试，不能完成真实的图片上传与确认：

```powershell
$env:APP_ENV = "development"
$env:HTTP_ADDR = "127.0.0.1:8080"
$env:DB_DSN = "postgres://bug:bug@127.0.0.1:5432/bug_feedback?sslmode=disable"
$env:REDIS_ADDR = "127.0.0.1:6379"
$env:FEEDBACK_SECRET_KEY = "local-dev-feedback-secret-at-least-32-bytes"
$env:ADMIN_API_TOKEN = "local-admin-token"

# COS 占位值。测试图片上传/确认时替换为真实凭据。
$env:S3_ACCESS_KEY = "local-test-access"
$env:S3_SECRET_KEY = "local-test-secret"
$env:S3_BUCKET = "local-test-1250000000"
$env:S3_REGION = "ap-guangzhou"
$env:S3_BASE_PREFIX = "uploads"
```

启动服务：

```powershell
go mod download
go run ./cmd/server
```

服务启动时会自动幂等执行 `migrations/schema.sql`。检查健康状态：

```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
```

停止原生依赖：

```powershell
redis-cli -p 6379 shutdown nosave
pg_ctl -D (Join-Path $env:USERPROFILE ".lens-backend\pgdata") -m fast stop
```

### Docker 构建与运行

```bash
# 构建镜像（构建阶段使用 goproxy.cn，产出纯静态二进制）
docker build -t bug-feedback-backend .

# 运行（DB/Redis 由上面的 docker compose 启动；macOS/Windows 用 host.docker.internal）
docker run -d --name bug-feedback -p 8080:8080 \
  -e APP_ENV=prod \
  -e DB_DSN='postgres://bug:bug@host.docker.internal:5432/bug_feedback?sslmode=disable' \
  -e REDIS_ADDR='host.docker.internal:6379' \
  -e FEEDBACK_SECRET_KEY='replace-with-a-long-random-secret' \
  -e S3_BUCKET='your-bucket-1250000000' \
  -e S3_ACCESS_KEY='your-secret-id' \
  -e S3_SECRET_KEY='your-secret-key' \
  -e ADMIN_API_TOKEN='change-me-to-a-long-random-token' \
  bug-feedback-backend

# 健康检查
curl -s http://localhost:8080/healthz
```

镜像以非 root 用户运行，内置 `HEALTHCHECK` 探测 `/healthz`；如需本地时区（如 `Asia/Shanghai`）可加 `-e TZ=Asia/Shanghai`。

## 环境变量

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `APP_ENV` | 否 | `development` | `development` 使用 gin DebugMode |
| `HTTP_ADDR` | 否 | `:8080` | 监听地址 |
| `TRUSTED_PROXIES` | 否 | 空 | 逗号分隔的可信代理 CIDR（反代后需正确设置，见「限流说明」） |
| `DB_DSN` | **是** | - | PostgreSQL 连接串（支持 `postgres://` 与 `postgresql://`） |
| `REDIS_ADDR` | 否 | `127.0.0.1:6379` | Redis 地址 |
| `REDIS_PASSWORD` | 否 | 空 | Redis 密码 |
| `REDIS_DB` | 否 | `0` | Redis DB |
| `FEEDBACK_SECRET_KEY` | **是** | - | 反馈中心 secret 的 AES-GCM 加密密钥；生产环境使用至少 32 字节随机值 |
| `FEEDBACK_CODE_TTL_SECONDS` | 否 | `300` | 校验码及 nonce 标记 TTL |
| `FEEDBACK_TIMESTAMP_WINDOW_SECONDS` | 否 | `300` | 请求 timestamp 允许的 ± 秒数 |
| `FEEDBACK_CENTER_CACHE_TTL_SECONDS` | 否 | `600` | 反馈中心 Redis 缓存 TTL |
| `FEEDBACK_RATE_LIMIT_PER_MINUTE` | 否 | `120` | 单反馈中心 + SN 每分钟校验次数 |
| `STORAGE_PROVIDER` | 否 | `s3` | 存储类型标识（固定 s3） |
| `S3_ACCESS_KEY` | **是** | - | COS SecretId |
| `S3_SECRET_KEY` | **是** | - | COS SecretKey |
| `S3_BUCKET` | **是** | - | 桶名（含 appid） |
| `S3_REGION` | 否 | `ap-guangzhou` | COS 地域 |
| `S3_CDN_URL` | 否 | 推导 | 桶访问域名 `https://{bucket}.cos.{region}.myqcloud.com`；不填则按 `S3_BUCKET`+`S3_REGION` 推导 |
| `S3_ENDPOINT` | 否 | - | 服务端点 `https://cos.{region}.myqcloud.com`（对象操作不使用） |
| `S3_BASE_PREFIX` | 否 | 空 | 对象 key 前缀（如 `uploads`） |
| `PRESIGN_GET_EXPIRE_SECONDS` | 否 | `900` | 详情图片 GET URL 有效期 |
| `UPLOAD_PRESIGN_EXPIRE_SECONDS` | 否 | `900` | 上传 PUT URL 有效期 |
| `IMAGE_MAX_SIZE` | 否 | `5242880` | 图片大小上限（字节） |
| `IMAGE_MAX_COUNT` | 否 | `6` | 单条反馈附件上限 |
| `CONTENT_MAX_LENGTH` | 否 | `2000` | 反馈内容字符上限 |
| `CONTACT_MAX_LENGTH` | 否 | `128` | 联系方式字符上限 |
| `ORPHAN_IMAGE_RETAIN_HOURS` | 否 | `24` | 孤儿图片保留时长 |
| `ADMIN_API_TOKEN` | **是** | - | 管理员 Bearer Token |
| `RATE_LIMIT_RPS` | 否 | `5` | 令牌桶速率 |
| `RATE_LIMIT_BURST` | 否 | `10` | 令牌桶容量 |
| `FEEDBACK_DAILY_LIMIT` | 否 | `100` | 单 client 24 小时提交上限 |
| `HTTP_TIMEOUT_SECONDS` | 否 | `90` | 单请求超时秒数（`0` 禁用；DB/COS 操作随请求取消） |
| `ADMIN_RATE_LIMIT_RPS` | 否 | `10` | admin 接口独立限流速率（防 token 爆破） |
| `ADMIN_RATE_LIMIT_BURST` | 否 | `30` | admin 接口限流桶容量 |
| `CORS_ALLOWED_ORIGINS` | 否 | 空 | 可选 CORS 白名单（逗号分隔，空=不输出 CORS 头） |

敏感信息（`S3_SECRET_KEY`、`ADMIN_API_TOKEN`、`FEEDBACK_SECRET_KEY`、反馈中心 secret 与完整 code）绝不打印到日志；请求日志只记录路径、请求 ID、中心 ID、SN、业务错误码和耗时。

## 接口说明

客户端与管理接口以 `/api/v1` 为主；反馈中心管理接口按 PRD 同时提供 `/api/admin`。统一响应：

管理员请求必须携带 `Authorization: Bearer <ADMIN_API_TOKEN>`；可选 `X-Operator` 记录审计操作人（默认 `admin`），`X-Admin-Role: readonly` 可限制为只读查询。

- 成功：`{"code":0,"message":"ok","data":{}}`
- 失败：`{"code":错误码,"message":"错误信息"}`

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/healthz` | 无 | 健康检查（含 DB 探测，DB 异常返回 503） |
| POST | `/uploads/presign` | `X-Client-ID` | 获取图片上传凭证 |
| POST | `/uploads/confirm` | `X-Client-ID` | 确认图片上传（Magic Number 校验） |
| POST | `/feedbacks` 或 `/feedback` | `X-Client-ID` + 反馈中心校验头 | 提交反馈（校验码一次性消费） |
| GET | `/admin/feedbacks` | Bearer Token | 分页 / contact 模糊 / app_name / 时间过滤 |
| GET | `/admin/feedbacks/:feedback_no` | Bearer Token | 反馈详情 + 图片临时 URL |
| POST | `/admin/feedback-centers` | Bearer Token | 创建反馈中心，secret 仅返回一次 |
| GET | `/admin/feedback-centers` | Bearer Token | 分页、关键词、状态筛选 |
| GET | `/admin/feedback-centers/:center_id` | Bearer Token | 详情（secret 脱敏） |
| PUT | `/admin/feedback-centers/:center_id` | Bearer Token | 更新名称、AppID、SN 模式等 |
| PATCH | `/admin/feedback-centers/:center_id/status` | Bearer Token | 更新启停状态 |
| POST | `/admin/feedback-centers/:center_id/enable` | Bearer Token | 启用 |
| POST | `/admin/feedback-centers/:center_id/disable` | Bearer Token | 禁用 |
| POST | `/admin/feedback-centers/:center_id/secret/reset` | Bearer Token | 重置 secret，旧校验码失效 |
| POST | `/admin/feedback-centers/:center_id/codes/generate` | Bearer Token | 生成测试校验码 |
| GET | `/admin/feedback-centers/:center_id/audit-logs` | Bearer Token | 查询审计日志 |

### 错误码

| HTTP | code | 说明 |
| --- | --- | --- |
| 400 | 40001 | 参数错误 |
| 400 | 40002 | 缺少 X-Client-ID；反馈中心 timestamp 格式错误 |
| 400 | 40003 | timestamp 超出允许窗口 |
| 400 | 40004 | SN 非法 |
| 401 | 40101 | 管理鉴权未通过 |
| 404 | 40101 | 反馈中心不存在 |
| 401 | 40102 | 反馈中心已禁用 |
| 401 | 40103 | secret 已重置，校验码失效 |
| 401 | 40005 | 校验码错误 |
| 409 | 40006 | 校验码已使用或 nonce 重放 |
| 404 | 40401 | 资源不存在 |
| 409 | 40901 | 重复提交 |
| 413 | 41301 | 文件过大 |
| 415 | 41501 | 文件类型不支持 |
| 429 | 42901 | 请求过于频繁 |
| 503 | 50001 | Redis 异常，校验服务暂不可用 |
| 500 | 50002 | 系统繁忙 |

## Flutter 客户端对接流程

### 分层职责

```text
应用层（光序 / 光汇）                    反馈 SDK（Flutter Package）
· 调用系统图片选择器                     · 校验 size ≤ 5MB、mime_type 合法
· 压缩（长边 ≤2000px, 质量 70~85）       · 封装 presign → PUT → confirm → submit
· 格式转换（HEIC/BMP → JPEG 等）         · 持久化 client_id（UUID）
· 确保最终产物 ≤ 5MB                    · 不做压缩、不做格式转换
```

压缩与格式转换**只在应用层**完成，SDK 仅做入参校验与 API 调用封装。

### SDK 接口（Dart）

```dart
class FeedbackSDK {
  /// 初始化：自动生成/读取 client_id（UUID），持久化保存，不得每次重新生成。
  static Future<void> init({required String baseUrl});

  /// 上传单张图片。调用方必须保证 imageBytes 已压缩且 ≤5MB，mimeType 合法。
  /// 若 imageBytes.length > 5MB 直接抛 ArgumentError，不发请求。
  Future<UploadResult> uploadImage({
    required Uint8List imageBytes,
    required String mimeType,
    String? filename,
  });

  /// 提交反馈。
  Future<SubmitResult> submitFeedback({
    required String content,
    required String contact,
    List<int> attachmentIds = const [],
    Map<String, dynamic>? extra,
  });
}
```

### 关键约定（务必遵守）

1. **匿名身份**：所有请求携带 `X-Client-ID` 头，值为 SDK 初始化时生成并持久化的 UUID，不得每次请求重新生成。
2. **size 必须精确**：调用 presign 时 `size` 必须等于 `imageBytes.length`。后端会把 `Content-Length` 钉进预签名，PUT 时实际字节数与声明不符会直接 403（见「私有桶安全说明」）。
3. **mime 严格对应**：压缩输出什么格式，`mimeType` 就填什么；PNG 透明图转 JPEG 会变黑底，透明截图建议保留 PNG 或转码前填充白底。
4. **请求幂等**：`submitFeedback` 的 `request_id` 由 SDK 每次提交生成一个唯一值（UUID），重试时复用同一值。

### 应用层压缩示例（flutter_image_compress）

```dart
import 'package:flutter_image_compress/flutter_image_compress.dart';

Future<Uint8List> compressForFeedback(File file) async {
  var result = await FlutterImageCompress.compressWithFile(
    file.absolute.path,
    minWidth: 2000, minHeight: 2000, quality: 80,
    format: CompressFormat.jpeg,
  );
  int quality = 80;
  while (result != null && result.lengthInBytes > 5 * 1024 * 1024 && quality > 20) {
    quality -= 20;
    result = await FlutterImageCompress.compressWithFile(
      file.absolute.path,
      minWidth: 1600, minHeight: 1600, quality: quality,
      format: CompressFormat.jpeg,
    );
  }
  if (result == null || result.lengthInBytes > 5 * 1024 * 1024) {
    throw Exception("图片尺寸过大，请尝试裁剪");
  }
  return result;
}
```

### 应用层调用 SDK 示例

```dart
final compressed = await compressForFeedback(selectedFile);
final upload = await FeedbackSDK.uploadImage(
  imageBytes: compressed,
  mimeType: 'image/jpeg',
  filename: 'screenshot.jpg',
);
final submit = await FeedbackSDK.submitFeedback(
  content: '课表页面切换周次后闪退。',
  contact: '13800000000',
  attachmentIds: [upload.attachmentId],
  extra: {'app_name': 'lumaris', 'app_version': '1.8.2'},
);
```

### 管理端图片展示

- 直接使用详情接口返回的 `images[].url` 赋给 Image 组件。
- URL 默认 15 分钟过期，页面停留过久会导致加载 403：前端应捕获该错误，提示「图片凭证已过期」，点击「刷新」重新请求详情接口获取新的临时 URL。

## 图片上传完整流程（后端视角）

1. 客户端调用 `POST /uploads/presign` 提交 `filename/size/mime_type`，后端先向 DB 插入一条 `unconfirmed` 附件记录（防幽灵文件），再返回 `file_key`、`upload_url`、`method`、`headers`（签名绑定 `Content-Type` 与 `Content-Length`）与 `expires_at`。
2. 客户端用 PUT URL 直传 COS，`Content-Type` 与 `Content-Length` 必须与申请时一致（实际上传字节数 == 声明 `size`），否则 COS 403。
3. 客户端调用 `POST /uploads/confirm`，后端：
   - 校验 `client_id` 归属；已 `confirmed` 幂等返回；
   - `HeadObject` 确认存在且 ≤5MB；
   - `GetObjectRange(bytes=0-15)` 读文件头，严格校验 Magic Number（JPEG `FF D8 FF` / PNG 8 字节签名 / WebP `RIFF...WEBP`）；
   - 校验失败：DB 标记 `invalid` 并返回 41501/41301（不立即删 COS，交由孤儿清理）；
   - 校验成功：标记 `confirmed`，返回 `attachment_id`。
4. 客户端调用 `POST /feedbacks` 携带 `attachment_ids`，后端在事务内完成幂等插入（`request_id` 去重，且仅同一 `client_id` 回显）与附件绑定。

## curl 联调示例

```bash
BASE=http://localhost:8080/api/v1
CID="test-client-001"
TOKEN="change-me-to-a-long-random-token"

# 1. 健康检查
curl -s http://localhost:8080/healthz

# 2. 创建反馈中心
CENTER=$(curl -s -X POST http://localhost:8080/api/admin/feedback-centers \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Operator: admin" \
  -H "Content-Type: application/json" \
  -d '{"name":"测试中心","appId":"app_123","env":"prod","snMode":"whitelist","snList":["SN001"],"expireAt":"2027-01-01T00:00:00Z","contact":"dev@example.com","remark":"local test"}')
CENTER_ID=$(echo "$CENTER" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["centerId"])')

# 3. 生成校验码
CODE=$(curl -s -X POST http://localhost:8080/api/admin/feedback-centers/$CENTER_ID/codes/generate \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"timestamp\":$(date +%s),\"sn\":\"SN001\",\"nonce\":\"nonce-$(date +%s)\",\"ttl\":300}")
TS=$(echo "$CODE" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["timestamp"])')
NONCE=$(echo "$CODE" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["nonce"])')
VERIFY_CODE=$(echo "$CODE" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["code"])')

# 4. 获取上传凭证（size 必须等于实际上传文件的字节数）
IMG=/path/to/shot.png
SIZE=$(wc -c < "$IMG" | tr -d ' ')
PRESIGN=$(curl -s -X POST $BASE/uploads/presign \
  -H "X-Client-ID: $CID" \
  -H "Content-Type: application/json" \
  -d "{\"filename\":\"shot.png\",\"size\":$SIZE,\"mime_type\":\"image/png\"}")
echo "$PRESIGN"
FILE_KEY=$(echo "$PRESIGN" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["file_key"])')
UPLOAD_URL=$(echo "$PRESIGN" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["upload_url"])')

# 5. 直接 PUT 上传（Content-Type 与 Content-Length 必须与 presign 声明一致）
curl -s -X PUT "$UPLOAD_URL" -H "Content-Type: image/png" --data-binary @"$IMG"

# 6. 确认上传
CONFIRM=$(curl -s -X POST $BASE/uploads/confirm \
  -H "X-Client-ID: $CID" \
  -H "Content-Type: application/json" \
  -d "{\"file_key\":\"$FILE_KEY\"}")
echo "$CONFIRM"
ATT_ID=$(echo "$CONFIRM" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["attachment_id"])')

# 7. 提交反馈（校验码一次有效）
curl -s -X POST $BASE/feedbacks \
  -H "X-Client-ID: $CID" \
  -H "X-Feedback-Center-Id: $CENTER_ID" \
  -H "X-Timestamp: $TS" \
  -H "X-SN: SN001" \
  -H "X-Nonce: $NONCE" \
  -H "X-Code: $VERIFY_CODE" \
  -H "Content-Type: application/json" \
  -d "{\"request_id\":\"req-$(date +%s)\",\"content\":\"登录闪退\",\"contact\":\"13800000000\",\"attachment_ids\":[$ATT_ID],\"extra\":{\"app_name\":\"edu\"}}"

# 8. 管理员列表（分页 + contact 模糊 + app_name + 时间范围，时间为 RFC3339）
curl -s "$BASE/admin/feedbacks?page=1&page_size=20&contact=138&app_name=edu&start_date=2026-08-01T00:00:00%2B08:00&end_date=2026-08-31T23:59:59%2B08:00" \
  -H "Authorization: Bearer $TOKEN"

# 9. 管理员详情（含图片预签名 URL）
curl -s "$BASE/admin/feedbacks/FBxxxx" -H "Authorization: Bearer $TOKEN"
```

## 限流说明

- 进程内令牌桶，维度为 `IP|X-Client-ID`，参数由 `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` 控制。
- **admin 接口独立限流**：`/admin` 路由先经独立 IP 限流器（`ADMIN_RATE_LIMIT_RPS`/`ADMIN_RATE_LIMIT_BURST`）再鉴权，防 token 爆破与误用。
- 触发限流返回 `429`（code `42901`），响应头携带 `Retry-After: 60`。
- 后台 Goroutine 每 5 分钟清理超过 10 分钟不活跃的 Limiter，防止内存泄漏。
- 另有 `FEEDBACK_DAILY_LIMIT`：同一 `client_id` 24 小时内成功反馈数上限（命中 `idx_feedback_client_time` 索引）。
- **反代部署注意**：若服务部署在 Nginx 等反代之后，务必正确配置 `TRUSTED_PROXIES`（如 `127.0.0.1/32` 或反代网段），否则 `ClientIP()` 取到的是反代 IP，会导致所有客户端共用一个限流桶。

## 孤儿图片清理说明

- 后台 `time.Ticker` 每 10 分钟执行一次。
- 查询条件：`(feedback_id IS NULL OR status != 'confirmed') AND created_at < now() - interval '24 hours'`（保留时长由 `ORPHAN_IMAGE_RETAIN_HOURS` 控制）。
- 单批最多 100 条：循环调用 COS 删除接口（`DeleteObject` 幂等），删除成功后才物理 DELETE 对应 DB 记录。
- 随服务优雅退出。

## 私有桶安全说明

- COS 桶**完全私有**，禁止任何公开读写与列举，绝不暴露固定 COS URL；即使 `file_key` 泄露，没有后端实时签名也无法下载图片。
- 所有图片访问均通过后端实时生成的 Presign GET URL（默认 15 分钟有效）。
- **体积约束（方案 B）**：COS 预签名不支持 `content-length-range` 区间，但通过把 `Content-Length` 钉死为 presign 声明的 `size`（服务端已强制 ≤5MB）纳入签名，客户端 PUT 的 `Content-Length` 必须精确匹配，否则 COS 直接 403 拒绝落盘，从签名层面杜绝超大文件上传。
- **网关兜底（方案 D）**：生产环境建议再叠加 Nginx `client_max_body_size 5m` 限制请求体，作为第二道防线：

```nginx
location / {
    client_max_body_size 5m;
    proxy_pass http://go-backend;
}
```

- 未带签名的图片直连 URL 应返回 403；详情图片 URL 16 分钟后再次访问也应返回 403。

## 部署指南

> 生产部署：配置写在仓库外的 env 文件，由 Docker 注入，不碰代码；密钥永不入库。

### 1. 服务器配置

在服务器上创建 env 文件（仓库外，`chmod 600`）：

```bash
mkdir -p /etc/bug-feedback
cp .env.example /etc/bug-feedback/backend.env
vim /etc/bug-feedback/backend.env   # 填入真实密钥
chmod 600 /etc/bug-feedback/backend.env
```

`backend.env` 示例（每行 `KEY=value`，行首 `#` 注释，不要加引号、不做变量展开）：

```ini
APP_ENV=production
HTTP_ADDR=:8080
TRUSTED_PROXIES=127.0.0.1/32
TZ=Asia/Shanghai

DB_DSN=postgres://bug:你的密码@127.0.0.1:5432/bug_feedback?sslmode=disable
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=
REDIS_DB=0

S3_ACCESS_KEY=你的SecretId
S3_SECRET_KEY=你的SecretKey
S3_BUCKET=your-bucket-1250000000
S3_REGION=ap-guangzhou
S3_CDN_URL=https://your-bucket-1250000000.cos.ap-guangzhou.myqcloud.com
S3_BASE_PREFIX=uploads

ADMIN_API_TOKEN=<用 openssl rand -hex 32 生成>
FEEDBACK_SECRET_KEY=<用 openssl rand -hex 32 生成>
```

### 2. 生产部署步骤（镜像由 GitHub Actions 构建）

镜像由 GitHub Actions 的 `build-and-release` 工作流构建（`linux/amd64`），导出 `.tar` 上传到 Release；服务器 `docker load` 导入即可，**无需在服务器编译**：

```bash
# 1. 下载 Release 附件 feedback-backend-linux-amd64.tar

# 2. 导入镜像
docker load -i feedback-backend-linux-amd64.tar

# 3. 运行
docker run -d --name bug-feedback -p 8080:8080 \
  --env-file /etc/bug-feedback/backend.env \
  --restart unless-stopped \
  feedback-backend:latest

# 4. 验证
curl -s http://localhost:8080/healthz   # 期望 {"code":0,"message":"ok","data":{}}
```

临时覆盖单个变量用 `-e`（优先于 env 文件）。服务器上直接构建（未走 GitHub Actions）时：`docker build -t feedback-backend:latest .`。

### 3. 验证环境变量注入

```bash
docker exec bug-feedback env | grep -E 'APP_ENV|DB_DSN|S3_|ADMIN_API'
```

### 4. 端到端联调

仓库内 `scripts/e2e_smoke.py`（纯 Python 标准库）可替代前端驱动全链路：presign → PUT 直传 COS → confirm → Magic Number 拦截 → 提交 → 幂等重放 → admin 列表/详情 → 图片下载。

```bash
python scripts/e2e_smoke.py   # 预期 12 通过, 0 失败
```

> `ADMIN_API_TOKEN` 必须是纯 ASCII（HTTP 头仅支持 Latin-1），不要用含中文的占位值。

### 5. 注意事项

- `docker inspect <容器>` 会明文显示所有环境变量，请保护好 Docker socket 权限。
- Dockerfile 的 `HEALTHCHECK` 固定探测 `127.0.0.1:8080/healthz`，改 `HTTP_ADDR` 端口需同步改 Dockerfile。
- 限流是进程内的，多副本部署时总限流会按副本数放大，需要全局限流请叠加网关层。
- `S3_CDN_URL` 可留空，代码会按 `S3_BUCKET + S3_REGION` 自动推导桶访问域名。
- 生产必改项：`APP_ENV=production`（否则 gin 为 DebugMode）；放在反代后必须设 `TRUSTED_PROXIES`，否则限流按反代 IP 生效。
