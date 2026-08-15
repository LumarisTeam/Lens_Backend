# 第三方教务软件 Bug 反馈中心后端

极简、高安全的匿名 Bug 反馈服务：Flutter 客户端（光序 / 光汇）匿名提交 Bug 描述、联系方式和图片；后端保存反馈内容与图片元数据；管理员通过受保护的接口查看反馈与图片。

**本期不做**：用户登录、用户历史反馈、反馈状态流转、评论回复、消息通知、统计报表、图片审核、缩略图、EXIF 清理。

## 目录结构

```text
.
├── cmd/server/main.go          # 启动、路由、优雅退出
├── internal/
│   ├── config/config.go        # 环境变量读取
│   ├── cos/client.go           # 腾讯云 COS 封装（Presign/Head/GetRange/Delete）
│   ├── handler/                # Gin Handlers (admin, feedback, upload, response)
│   ├── middleware/             # auth, client_id, ratelimit, recovery, body_limit, logger
│   ├── model/model.go          # DB Models & DTOs
│   ├── repository/             # pgx/v5 数据访问层
│   ├── service/                # 业务逻辑（含 orphan_cleaner、ratelimit）
│   └── pkg/                    # idgen (ULID), imagecheck (Magic Number), apperr
├── migrations/schema.sql       # embed 执行
├── docker-compose.yml          # 本地 PostgreSQL 环境
├── Makefile                    # 构建与代理配置
└── .env.example                # 环境变量样例
```

## 技术栈

- Go 1.22+ 单体服务（中国大陆网络环境，`Makefile` 已配置 `GOPROXY=https://goproxy.cn,direct`）
- Web 框架：`gin`
- 数据库：PostgreSQL + `pgx/v5`（`pgxpool` 连接池，禁止 ORM）
- 对象存储：腾讯云 COS（`cos-go-sdk-v5`，完全私有桶）
- 限流：`golang.org/x/time/rate`（进程内令牌桶）
- 标准库：`crypto/rand` + `encoding/base32`（ULID）、`embed`（迁移）、`os`（配置）、`log/slog`（JSON 日志）、`time`（定时任务）

## 快速开始

```bash
# 1. 启动本地 PostgreSQL
docker compose up -d

# 2. 配置环境变量（参考 .env.example）
export DB_DSN='postgres://bug:bug@localhost:5432/bug_feedback?sslmode=disable'
export COS_REGION='ap-guangzhou'
export COS_BUCKET='your-bucket-1250000000'
export COS_ACCESS_KEY='your-secret-id'
export COS_SECRET_KEY='your-secret-key'
export ADMIN_API_TOKEN='change-me-to-a-long-random-token'

# 3. 整理依赖并运行
make tidy
make run
```

启动后 `GET /healthz` 返回 `{"code":0,"message":"ok","data":{}}`，并在启动时幂等执行 `migrations/schema.sql` 建表（无需第三方迁移工具）。

## 环境变量

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `APP_ENV` | 否 | `development` | `development` 使用 gin DebugMode |
| `HTTP_ADDR` | 否 | `:8080` | 监听地址 |
| `TRUSTED_PROXIES` | 否 | 空 | 逗号分隔的可信代理 CIDR（反代后需正确设置，见「限流说明」） |
| `DB_DSN` | **是** | - | PostgreSQL 连接串 |
| `COS_REGION` | 否 | `ap-guangzhou` | COS 地域 |
| `COS_BUCKET` | **是** | - | COS 桶名 |
| `COS_ENDPOINT` | 否 | 推导 | 完整桶域名 `https://{bucket}.cos.{region}.myqcloud.com`；不填则按 `COS_BUCKET`+`COS_REGION` 推导（勿填不含桶名的服务端点） |
| `COS_ACCESS_KEY` | **是** | - | SecretId |
| `COS_SECRET_KEY` | **是** | - | SecretKey |
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

敏感信息（`COS_SECRET_KEY`、`ADMIN_API_TOKEN`）只用于鉴权，绝不打印到日志；日志只记录请求路径，不记录查询串与请求头。

## 接口说明

所有接口前缀 `/api/v1`。统一响应：

- 成功：`{"code":0,"message":"ok","data":{}}`
- 失败：`{"code":错误码,"message":"错误信息"}`

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/healthz` | 无 | 健康检查 |
| POST | `/uploads/presign` | `X-Client-ID` | 获取图片上传凭证 |
| POST | `/uploads/confirm` | `X-Client-ID` | 确认图片上传（Magic Number 校验） |
| POST | `/feedbacks` | `X-Client-ID` | 提交反馈 |
| GET | `/admin/feedbacks` | Bearer Token | 分页 / contact 模糊 / app_name / 时间过滤 |
| GET | `/admin/feedbacks/:feedback_no` | Bearer Token | 反馈详情 + 图片临时 URL |

### 错误码

| HTTP | code | 说明 |
| --- | --- | --- |
| 400 | 40001 | 参数错误 |
| 400 | 40002 | 缺少 X-Client-ID |
| 401 | 40101 | 未授权 |
| 404 | 40401 | 资源不存在 |
| 409 | 40901 | 重复提交 |
| 413 | 41301 | 文件过大 |
| 415 | 41501 | 文件类型不支持 |
| 429 | 42901 | 请求过于频繁 |
| 500 | 50001 | 服务内部错误 |

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
curl -s $BASE/healthz

# 2. 获取上传凭证（size 必须等于实际上传文件的字节数）
IMG=/path/to/shot.png
SIZE=$(wc -c < "$IMG" | tr -d ' ')
PRESIGN=$(curl -s -X POST $BASE/uploads/presign \
  -H "X-Client-ID: $CID" \
  -H "Content-Type: application/json" \
  -d "{\"filename\":\"shot.png\",\"size\":$SIZE,\"mime_type\":\"image/png\"}")
echo "$PRESIGN"
FILE_KEY=$(echo "$PRESIGN" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["file_key"])')
UPLOAD_URL=$(echo "$PRESIGN" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["upload_url"])')

# 3. 直接 PUT 上传（Content-Type 与 Content-Length 必须与 presign 声明一致）
curl -s -X PUT "$UPLOAD_URL" -H "Content-Type: image/png" --data-binary @"$IMG"

# 4. 确认上传
CONFIRM=$(curl -s -X POST $BASE/uploads/confirm \
  -H "X-Client-ID: $CID" \
  -H "Content-Type: application/json" \
  -d "{\"file_key\":\"$FILE_KEY\"}")
echo "$CONFIRM"
ATT_ID=$(echo "$CONFIRM" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["attachment_id"])')

# 5. 提交反馈
curl -s -X POST $BASE/feedbacks \
  -H "X-Client-ID: $CID" \
  -H "Content-Type: application/json" \
  -d "{\"request_id\":\"req-$(date +%s)\",\"content\":\"登录闪退\",\"contact\":\"13800000000\",\"attachment_ids\":[$ATT_ID],\"extra\":{\"app_name\":\"edu\"}}"

# 6. 管理员列表（分页 + contact 模糊 + app_name + 时间范围，时间为 RFC3339）
curl -s "$BASE/admin/feedbacks?page=1&page_size=20&contact=138&app_name=edu&start_date=2026-08-01T00:00:00%2B08:00&end_date=2026-08-31T23:59:59%2B08:00" \
  -H "Authorization: Bearer $TOKEN"

# 7. 管理员详情（含图片预签名 URL）
curl -s "$BASE/admin/feedbacks/FBxxxx" -H "Authorization: Bearer $TOKEN"
```

## 限流说明

- 进程内令牌桶，维度为 `IP|X-Client-ID`，参数由 `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` 控制。
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
