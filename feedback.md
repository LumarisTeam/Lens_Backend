# 反馈中心注册与 Redis 校验码管理 PRD

## 1. 文档信息

| 项目 | 内容 |
|---|---|
| 需求名称 | 反馈中心注册与 Redis 校验码管理 |
| 涉及端 | React 管理后台、Go 后端服务、Redis、MySQL、Flutter SDK/业务客户端 |
| 版本 | V1.0 |
| 状态 | 草案 |
| 优先级 | P0 |
| 关联文档 | 《用户反馈提交功能 PRD V1.1》 |

---

## 2. 背景与目标

### 2.1 背景

现有用户反馈提交功能依赖 `AppID + 业务Token` 完成身份解析。为增强反馈入口的安全性、防伪造、防重放能力，需要建设“反馈中心注册 + 校验码”机制。

反馈中心由管理后台注册，后端 Go 服务生成唯一 `centerId` 和 `secret`。客户端或业务服务端基于 `centerId + timestamp + sn + nonce` 生成校验码，Redis 负责校验码存储、一次性消费、防重放和短 TTL 管理。

### 2.2 目标

- React 管理后台支持反馈中心注册、查询、启停、重置密钥、生成测试校验码。
- Go 后端提供反馈中心管理 API 和校验码生成/校验能力。
- Redis 实现校验码存储、TTL、防重放、限流。
- 校验码基于反馈中心注册的 `centerId`、时间戳 `timestamp`、SN 号 `sn`、随机数 `nonce` 等生成。
- 反馈提交接口接入校验码校验，校验通过后再进入 `UserIdentityResolver` 和 `FeedbackService`。
- 支持错误码、审计日志、监控和验收。

### 2.3 成功指标

- 校验码生成成功率 ≥ 99.9%。
- 校验码校验成功率 ≥ 99.9%。
- 校验接口 P95 ≤ 50ms。
- Redis 校验码 TTL 准确率 100%。
- 重放请求拦截率 100%。
- 管理后台注册反馈中心成功率 ≥ 99.9%。

---

## 3. 范围

### 3.1 包含

- React 管理后台：反馈中心列表、新建、详情、启停、重置 secret、生成测试校验码、审计日志。
- Go 后端：反馈中心管理 API、校验码生成 API、校验中间件、Redis 操作、MySQL 持久化。
- Redis：校验码、nonce、注册信息缓存、限流计数。
- 校验码算法：HMAC-SHA256。
- 与现有反馈提交接口集成。
- 错误码、日志、监控、验收标准。

### 3.2 不包含

- 客服工单、反馈回复、审核工作流。
- 复杂 RBAC 权限体系，首期仅区分管理员和只读用户。
- 多语言完整适配，管理后台首期中文。
- 第三方风控系统对接。

---

## 4. 术语与核心概念

| 术语 | 说明 |
|---|---|
| centerId | 反馈中心注册后生成的唯一 ID，如 `fc_xxx` |
| secret | 反馈中心密钥，用于生成校验码，仅创建时展示一次 |
| timestamp | 请求时间戳，Unix 秒 |
| sn | 业务方设备/实例序列号，用于标识调用来源 |
| nonce | 随机字符串，UUID，用于防重放 |
| code | 校验码，基于 centerId、timestamp、sn、nonce、secret 生成 |
| TTL | Redis 键过期时间，默认 300 秒 |

---

## 5. 核心流程

```text
[React 管理后台]
      │ 注册反馈中心
      ▼
[Go 后端]
      ├── 生成 centerId、secret
      ├── 写入 MySQL feedback_center
      ├── 缓存 Redis fc:center:{centerId}
      └── 返回 centerId、secret

[业务客户端 / Flutter SDK]
      │ 获取或生成校验码
      ▼
[Go 校验码服务]
      ├── 校验 centerId 是否存在且启用
      ├── 校验 timestamp 是否在允许窗口内
      ├── 校验 sn 是否合法
      ├── 校验 code 是否匹配 Redis
      ├── 校验 nonce 是否已使用
      └── 通过后放行到反馈提交接口
```

---

## 6. 功能需求

### 6.1 React 管理后台

| 编号 | 需求 | 说明 |
|---|---|---|
| R-01 | 登录与权限 | 管理员登录后可管理反馈中心，只读用户仅查看 |
| R-02 | 反馈中心列表 | 支持分页、搜索 centerId/名称/AppID、筛选状态 |
| R-03 | 新建反馈中心 | 填写名称、AppID、环境、SN 模式、有效期、联系人、备注 |
| R-04 | 生成注册信息 | 后端返回 centerId、secret，secret 仅展示一次 |
| R-05 | 详情页 | 展示 centerId、AppID、状态、SN 列表、创建时间、最近调用 |
| R-06 | 启停操作 | 启用/禁用反馈中心，禁用后校验码全部拒绝 |
| R-07 | 重置 secret | 重置后旧 secret 生成的校验码全部失效 |
| R-08 | 生成测试校验码 | 输入 timestamp、sn、nonce、TTL，调用后端生成 code |
| R-09 | 审计日志 | 记录创建、修改、启停、重置 secret、生成 code 等操作 |
| R-10 | 脱敏展示 | secret、code 脱敏，日志不输出完整 secret |

### 6.2 Go 后端服务

| 编号 | 需求 | 说明 |
|---|---|---|
| G-01 | 反馈中心管理 API | 创建、列表、详情、更新、启停、重置 secret |
| G-02 | 校验码生成 API | 根据 centerId、timestamp、sn、nonce 生成 code 并写 Redis |
| G-03 | 校验中间件 | 反馈提交接口前校验 centerId、timestamp、sn、nonce、code |
| G-04 | Redis 操作 | SET、GET、SETNX、DEL、EXPIRE，保证原子性 |
| G-05 | MySQL 持久化 | 存储反馈中心、SN、审计日志 |
| G-06 | 统一响应 | `code`、`message`、`data` |
| G-07 | 异常处理 | Redis 异常、时间戳过期、重放、SN 非法等明确错误码 |
| G-08 | 日志监控 | 记录 requestId、centerId、sn、耗时、错误码，不记录 secret 明文 |

### 6.3 Redis 校验码实现

**校验码算法：**

```text
code = Base64URL(HMAC_SHA256(secret, centerId + "|" + timestamp + "|" + sn + "|" + nonce))
```

**生成流程：**

1. 后端校验 centerId 存在且启用。
2. 校验 sn 是否在允许范围内。
3. 生成 nonce 或使用请求传入 nonce。
4. 计算 code。
5. 写入 Redis：
   ```text
   SET fc:code:{centerId}:{sn}:{timestamp}:{nonce} = code EX 300
   ```
6. 返回 code、expireAt。

**校验流程：**

1. 读取请求头：
    - `X-Feedback-Center-Id`
    - `X-Timestamp`
    - `X-SN`
    - `X-Nonce`
    - `X-Code`
2. 校验 timestamp 是否在 ±300 秒内。
3. 查询 Redis 或 MySQL 校验 centerId 状态。
4. 校验 sn 合法性。
5. 查询 Redis `fc:code:{...}` 并与请求 code 比对。
6. 使用 `SETNX fc:nonce:{centerId}:{sn}:{nonce} 1 EX 300` 防重放。
7. 校验通过后删除 code，保证一次性使用。
8. 放行到反馈提交接口。

> 首期采用“平台生成校验码 + Redis 存储 + 一次性消费”模式。后续可扩展“业务方本地 HMAC 签名 + Redis 仅存 nonce”模式。

---

## 7. 接口定义

### 7.1 创建反馈中心

`POST /api/admin/feedback-centers`

请求：

```json
{
  "name": "测试反馈中心",
  "appId": "app_123",
  "env": "prod",
  "snMode": "whitelist",
  "snList": ["SN001", "SN002"],
  "expireAt": "2027-01-01T00:00:00Z",
  "contact": "dev@example.com",
  "remark": "备注"
}
```

响应：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "centerId": "fc_abc123",
    "secret": "sk_xxxxxxxx",
    "status": "enabled"
  }
}
```

### 7.2 生成校验码

`POST /api/admin/feedback-centers/{centerId}/codes/generate`

请求：

```json
{
  "timestamp": 1758888888,
  "sn": "SN001",
  "nonce": "uuid-xxxx",
  "ttl": 300
}
```

响应：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "centerId": "fc_abc123",
    "timestamp": 1758888888,
    "sn": "SN001",
    "nonce": "uuid-xxxx",
    "code": "Base64UrlCode",
    "expireAt": "2026-09-26T12:05:00Z"
  }
}
```

### 7.3 客户端取码

`POST /api/v1/feedback-centers/{centerId}/codes`

请求头必须携带 `X-Client-ID`；secret 不下发客户端。

请求：

```json
{
  "sn": "SN001"
}
```

服务端生成 `timestamp`、`nonce` 和 `code`，并将 code 与该 `X-Client-ID` 绑定。提交反馈时必须使用同一个 `X-Client-ID`，否则校验失败。

SDK 的 `request_id` 仍是提交幂等键：重试时复用同一个 `request_id`，但重新取一枚新 code，不复用已消费的旧 code。

### 7.4 反馈提交接口增加校验头

`POST /api/v1/feedback`

新增请求头：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| X-Feedback-Center-Id | string | 是 | 反馈中心 ID |
| X-Timestamp | int64 | 是 | Unix 秒 |
| X-SN | string | 是 | 设备/实例 SN |
| X-Nonce | string | 是 | 随机字符串 |
| X-Code | string | 是 | 校验码 |

### 7.5 错误码

| 错误码 | 含义 | 客户端提示 |
|---|---|---|
| 0 | 成功 | 成功 |
| 40001 | 参数缺失 | 请求参数不完整 |
| 40002 | 时间戳格式错误 | 请求时间格式错误 |
| 40003 | 时间戳过期 | 请求已过期，请重试 |
| 40004 | SN 非法 | 设备标识无效 |
| 40005 | 校验码错误 | 校验失败 |
| 40006 | 校验码已使用 | 请勿重复提交 |
| 40101 | 反馈中心不存在 | 反馈中心不存在 |
| 40102 | 反馈中心已禁用 | 反馈中心已停用 |
| 40103 | secret 已重置 | 密钥已更新，请重新获取 |
| 42901 | 请求过于频繁 | 操作过于频繁，请稍后再试 |
| 50001 | Redis 异常 | 校验服务暂不可用 |
| 50002 | 系统繁忙 | 系统繁忙，请稍后重试 |

---

## 8. Redis 设计

| Key | 类型 | 说明 | TTL |
|---|---|---|---|
| `fc:center:{centerId}` | Hash | 缓存反馈中心信息：appId、status、secretVersion、snMode | 600s |
| `fc:code:{centerId}:{sn}:{timestamp}:{nonce}` | String | 校验码 | 300s |
| `fc:nonce:{centerId}:{sn}:{nonce}` | String | 防重放标记，SETNX | 300s |
| `fc:rate:{centerId}:{sn}:{minute}` | String | 限流计数 | 60s |
| `fc:lock:reset:{centerId}` | String | 重置 secret 分布式锁 | 10s |

**原子操作要求：**

- 生成校验码：`SET key code EX ttl`
- 防重放：`SETNX nonceKey 1 EX ttl`
- 消费校验码：校验通过后 `DEL codeKey`
- 限流：`INCR` + `EXPIRE`
- 重置 secret：加锁后更新 MySQL，再删除 Redis 缓存

**降级策略：**

- Redis 不可用时，校验码功能返回 `50001`。
- 若配置允许降级，可动态 HMAC 校验，但必须关闭防重放并记录高风险告警。
- 默认不降级，保证安全优先。

---

## 9. 数据模型

### 9.1 反馈中心表 `feedback_center`

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint | 主键 |
| center_id | varchar(64) | 反馈中心唯一 ID |
| name | varchar(128) | 名称 |
| app_id | varchar(64) | 业务 AppID |
| secret_cipher | varchar(256) | 加密后的 secret |
| secret_version | int | 密钥版本 |
| sn_mode | varchar(32) | whitelist/prefix/any |
| status | varchar(16) | enabled/disabled |
| expire_at | datetime | 过期时间 |
| contact | varchar(128) | 联系人 |
| remark | varchar(255) | 备注 |
| created_by | varchar(64) | 创建人 |
| created_at | datetime | 创建时间 |
| updated_at | datetime | 更新时间 |

### 9.2 SN 表 `feedback_center_sn`

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint | 主键 |
| center_id | varchar(64) | 反馈中心 ID |
| sn | varchar(128) | SN 号 |
| status | varchar(16) | enabled/disabled |
| expire_at | datetime | 过期时间 |
| created_at | datetime | 创建时间 |

### 9.3 审计表 `feedback_center_audit`

| 字段 | 类型 | 说明 |
|---|---|---|
| id | bigint | 主键 |
| center_id | varchar(64) | 反馈中心 ID |
| action | varchar(64) | create/update/enable/disable/reset_secret/generate_code |
| operator | varchar(64) | 操作人 |
| detail | json | 操作详情 |
| ip | varchar(64) | 操作 IP |
| created_at | datetime | 创建时间 |

---

## 10. 异常与边界

| 场景 | 处理 |
|---|---|
| centerId 缺失 | 返回 40001 |
| centerId 不存在 | 返回 40101 |
| 反馈中心已禁用 | 返回 40102 |
| timestamp 缺失 | 返回 40001 |
| timestamp 超过 ±300s | 返回 40003 |
| sn 未注册 | 返回 40004 |
| code 不匹配 | 返回 40005 |
| nonce 已使用 | 返回 40006 |
| Redis 不可用 | 返回 50001，记录告警 |
| MySQL 写入失败 | 返回 50002 |
| secret 重置 | 旧 code 全部失效，返回 40103 |
| 并发重复请求 | Redis SETNX 保证只有一个成功 |
| 管理后台重复点击 | 前端按钮置灰 + 后端幂等 |

---

## 11. 非功能需求

### 11.1 性能

- 校验接口 P95 ≤ 50ms。
- Redis 单次操作 P95 ≤ 10ms。
- 支持水平扩展，无本地状态。
- 反馈中心信息优先读 Redis，未命中查 MySQL。

### 11.2 安全

- 全链路 HTTPS。
- secret 加密存储，前端仅创建时展示一次。
- 日志不记录 secret、完整 code。
- 防重放：timestamp + nonce + Redis SETNX。
- 限流：按 centerId、sn、IP 限流。
- 错误信息不暴露内部实现。
- 重置 secret 后旧校验码全部失效。

### 11.3 日志与监控

- 记录 requestId、centerId、sn、timestamp、耗时、错误码。
- 不记录 secret 明文、完整 code。
- 监控：
    - 校验码生成 QPS
    - 校验成功率
    - 重放拦截数
    - Redis 异常数
    - P95 耗时

### 11.4 多语言

- 管理后台首期中文。
- 预留 i18n 字段。
- 错误码 message 支持后续多语言。

---

## 12. 埋点建议

| 事件 | 触发时机 | 关键参数 |
|---|---|---|
| feedback_center_create | 创建反馈中心 | appId、centerId、operator |
| feedback_center_enable | 启用 | centerId |
| feedback_center_disable | 禁用 | centerId |
| feedback_center_reset_secret | 重置 secret | centerId、operator |
| feedback_code_generate | 生成校验码 | centerId、sn、ttl |
| feedback_code_verify_success | 校验成功 | centerId、sn、耗时 |
| feedback_code_verify_fail | 校验失败 | centerId、sn、errorCode |
| feedback_code_replay | 重放拦截 | centerId、sn、nonce |

---

## 13. 验收标准

- [ ] React 管理后台可创建反馈中心，生成唯一 centerId 和 secret。
- [ ] secret 仅创建时展示，后续脱敏。
- [ ] Go 后端可生成校验码并写入 Redis，TTL 正确。
- [ ] 校验码基于 centerId、timestamp、sn、nonce、secret 生成。
- [ ] 反馈提交携带正确校验头时，校验通过并进入原反馈流程。
- [ ] timestamp 超过 ±300 秒时校验失败。
- [ ] 重复 nonce 或重复 code 被拦截。
- [ ] SN 非法时校验失败。
- [ ] 反馈中心禁用后，所有校验码拒绝。
- [ ] 重置 secret 后，旧 secret 生成的校验码全部失效。
- [ ] Redis 不可用时返回明确错误码并告警。
- [ ] 管理后台操作写入审计日志。
- [ ] 日志中无 secret 明文、无完整 code。
- [ ] 校验接口 P95 ≤ 50ms。
- [ ] 错误码与前端提示一致。

---

