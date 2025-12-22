# 管理员 API Key 管理接口文档

## 概述

这组 API 允许具有管理员权限的用户通过 access token 为任意用户创建、查看、删除 API Key（Token）。这些 Token 是用于访问模型 API 的凭证。

## 认证方式

所有接口需要在请求头中携带管理员账户的 access token：

```
Authorization: Bearer <admin_access_token>
```

## 基础路径

```
/api/admin/token
```

## API 端点

### 1. 获取用户的所有 Token

获取指定用户的所有 Token 列表，支持分页。

**请求**

```
GET /api/admin/token/user/:user_id
```

**路径参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | int | 是 | 目标用户 ID |

**查询参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| p | int | 否 | 页码，默认 1 |
| page_size | int | 否 | 每页数量，默认 10 |

**响应示例**

```json
{
  "success": true,
  "data": {
    "page": 1,
    "page_size": 10,
    "total": 2,
    "items": [
      {
        "id": 1,
        "user_id": 123,
        "key": "xxxxxxxxxxxxxxxx",
        "status": 1,
        "name": "My Token",
        "created_time": 1703145600,
        "accessed_time": 1703145600,
        "expired_time": -1,
        "remain_quota": 10000,
        "used_quota": 500,
        "unlimited_quota": false,
        "model_limits_enabled": false,
        "model_limits": "",
        "allow_ips": "",
        "group": ""
      }
    ]
  }
}
```

---

### 2. 搜索用户的 Token

按关键字搜索指定用户的 Token。

**请求**

```
GET /api/admin/token/user/:user_id/search
```

**路径参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | int | 是 | 目标用户 ID |

**查询参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| keyword | string | 否 | 按 Token 名称搜索 |
| token | string | 否 | 按 Token Key 搜索 |

**响应示例**

```json
{
  "success": true,
  "message": "",
  "data": [
    {
      "id": 1,
      "user_id": 123,
      "name": "My Token",
      "status": 1
    }
  ]
}
```

---

### 3. 获取 Token 详情

获取指定 Token 的详细信息。

**请求**

```
GET /api/admin/token/:id
```

**路径参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | int | 是 | Token ID |

**响应示例**

```json
{
  "success": true,
  "message": "",
  "data": {
    "id": 1,
    "user_id": 123,
    "key": "xxxxxxxxxxxxxxxx",
    "status": 1,
    "name": "My Token",
    "created_time": 1703145600,
    "accessed_time": 1703145600,
    "expired_time": -1,
    "remain_quota": 10000,
    "used_quota": 500,
    "unlimited_quota": false,
    "model_limits_enabled": false,
    "model_limits": "",
    "allow_ips": "",
    "group": ""
  }
}
```

---

### 4. 为用户创建 Token

为指定用户创建新的 API Token。

**请求**

```
POST /api/admin/token/user/:user_id
```

**路径参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | int | 是 | 目标用户 ID |

**请求体**

```json
{
  "name": "Token名称",
  "expired_time": -1,
  "remain_quota": 100000,
  "unlimited_quota": false,
  "model_limits_enabled": false,
  "model_limits": "",
  "allow_ips": "",
  "group": ""
}
```

**请求体字段说明**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | Token 名称（最长 30 字符） |
| expired_time | int64 | 否 | 过期时间戳（秒），-1 表示永不过期 |
| remain_quota | int | 否 | 剩余额度 |
| unlimited_quota | bool | 否 | 是否无限额度，默认 false |
| model_limits_enabled | bool | 否 | 是否启用模型限制，默认 false |
| model_limits | string | 否 | 允许的模型列表，逗号分隔 |
| allow_ips | string | 否 | IP 白名单，换行分隔 |
| group | string | 否 | 分组 |

**响应示例**

```json
{
  "success": true,
  "message": "",
  "data": {
    "id": 10,
    "user_id": 123,
    "key": "sk-xxxxxxxxxxxxxxxx",
    "name": "Token名称",
    "status": 1,
    "created_time": 1703145600,
    "accessed_time": 1703145600,
    "expired_time": -1,
    "remain_quota": 100000,
    "used_quota": 0,
    "unlimited_quota": false
  }
}
```

---

### 5. 更新 Token

更新指定 Token 的信息。

**请求**

```
PUT /api/admin/token
```

**查询参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| status_only | string | 否 | 如果设置（任意值），则只更新状态 |

**请求体**

```json
{
  "id": 10,
  "name": "新名称",
  "status": 1,
  "expired_time": -1,
  "remain_quota": 200000,
  "unlimited_quota": false,
  "model_limits_enabled": false,
  "model_limits": "",
  "allow_ips": "",
  "group": ""
}
```

**请求体字段说明**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | int | 是 | Token ID |
| name | string | 否 | Token 名称（最长 30 字符） |
| status | int | 否 | Token 状态 |
| expired_time | int64 | 否 | 过期时间戳（秒），-1 表示永不过期 |
| remain_quota | int | 否 | 剩余额度 |
| unlimited_quota | bool | 否 | 是否无限额度 |
| model_limits_enabled | bool | 否 | 是否启用模型限制 |
| model_limits | string | 否 | 允许的模型列表，逗号分隔 |
| allow_ips | string | 否 | IP 白名单，换行分隔 |
| group | string | 否 | 分组 |

**响应示例**

```json
{
  "success": true,
  "message": "",
  "data": {
    "id": 10,
    "user_id": 123,
    "name": "新名称",
    "status": 1,
    "remain_quota": 200000
  }
}
```

---

### 6. 删除 Token

删除指定的 Token。

**请求**

```
DELETE /api/admin/token/:id
```

**路径参数**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | int | 是 | Token ID |

**响应示例**

```json
{
  "success": true,
  "message": ""
}
```

---

## Token 状态值

| 值 | 常量名 | 说明 |
|----|--------|------|
| 1 | TokenStatusEnabled | 启用 |
| 2 | TokenStatusDisabled | 禁用 |
| 3 | TokenStatusExpired | 已过期 |
| 4 | TokenStatusExhausted | 额度耗尽 |

---

## 错误响应

所有接口在发生错误时返回以下格式：

```json
{
  "success": false,
  "message": "错误信息"
}
```

常见错误信息：

| 错误信息 | 说明 |
|----------|------|
| 无效的用户ID | 提供的 user_id 参数无效 |
| 无效的Token ID | 提供的 Token ID 参数无效 |
| 用户不存在 | 指定的用户不存在 |
| 令牌名称过长 | Token 名称超过 30 个字符 |
| 令牌已过期，无法启用 | 尝试启用已过期的 Token |
| 令牌可用额度已用尽，无法启用 | 尝试启用额度已耗尽的 Token |

---

## 使用示例

### cURL 示例

**创建 Token**

```bash
curl -X POST "http://your-api-host/api/admin/token/user/123" \
  -H "Authorization: Bearer your_admin_access_token" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Token",
    "expired_time": -1,
    "remain_quota": 100000,
    "unlimited_quota": false
  }'
```

**获取用户的所有 Token**

```bash
curl -X GET "http://your-api-host/api/admin/token/user/123?p=1&page_size=10" \
  -H "Authorization: Bearer your_admin_access_token"
```

**删除 Token**

```bash
curl -X DELETE "http://your-api-host/api/admin/token/10" \
  -H "Authorization: Bearer your_admin_access_token"
```
