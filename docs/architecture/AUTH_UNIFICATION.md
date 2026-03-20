# 鉴权体系优化与统一方案 (Authentication Architecture Unification)

## 1. 现状与问题分析

目前系统中存在三套并行的鉴权逻辑，导致维护成本高、安全审计困难且概念混淆。

### 1.1 三套鉴权逻辑

1.  **API Key (Machine)**: 长期有效，基于数据库 Hash 校验，无状态但需查库。
    - _位置_: `internal/middleware/oauth_client_auth.go`
    - _痛点_: 泄露风险高（需手动轮换），缺乏动态 Scope 控制。
2.  **OAuth2 Client Credentials (Machine)**: 短期有效，基于 JWT，支持动态 Scope。
    - _位置_: `internal/api/auth_handler.go` (Token颁发) + 中间件校验
    - _痛点_: 客户端实现复杂（需获取 Token），后端维护成本高（Token存储/状态管理）。
3.  **User Session/JWT (Human)**: 控制台用户登录。
    - _位置_: `internal/middleware/jwt_auth.go`
    - _痛点_: 与机器鉴权逻辑割裂，部分管理接口可能混用。

### 1.2 核心问题

- **逻辑冗余**: API Key 和 OAuth Client 本质都是机器身份，却维护了两套表结构和验证逻辑。
- **中间件复杂**: `APIOrOAuthAuth` 试图兼容两者，导致代码分支多，难以扩展（如统一添加 IP 白名单或计费逻辑）。
- **计费困难**: 计费逻辑分散在各个 Handler 或 Service 中，缺乏统一的 Metering 入口。

---

## 2. 演进目标：统一架构 (Unified Authentication)

目标是将“机器鉴权”收敛为一套逻辑，对上层业务屏蔽差异，并为未来的计费系统打好基础。

### 2.1 核心原则

- **Identity Unification**: API Key 视为一种“长期有效的 OAuth Client”或“特殊的 Token”。
- **Stateless Verification**: 优先使用 JWT 进行无状态校验，减少数据库压力。
- **Unified Context**: 无论鉴权方式如何，最终向 Context 注入统一的 `AuthContext` (OrgID, Scopes, IdentityID)。

### 2.2 推荐方案：Unified Gatekeeper

#### 第一阶段：中间件统一 (The Gatekeeper)

重构 `APIOrOAuthAuth` 中间件，使其成为统一的鉴权入口：

```go
func UnifiedAuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 1. 提取凭证 (Bearer Token 或 API Key)
        token, authType := extractCredential(c)

        // 2. 验证凭证
        var identity *Identity
        if authType == "Bearer" {
            identity = verifyJWT(token) // OAuth 路径
        } else if authType == "APIKey" {
            identity = verifyAPIKey(token) // API Key 路径
        }

        // 3. 注入统一上下文
        // 关键：业务层不再关心是 Key 还是 Token，只认 OrgID 和 Permissions
        c.Set("AuthContext", &AuthContext{
            OrgID:       identity.OrgID,
            UserID:      identity.UserID, // 或 ClientID
            Permissions: identity.Scopes,
            IsMachine:   true,
        })

        // 兼容遗留的 scope 校验逻辑 (如 middleware.RequireKeyScope)
        c.Set("scopes", identity.Scopes)

        c.Next()
    }
}
```

#### 第二阶段：模型融合 (Model Convergence)

- **数据层**: 考虑合并 `api_keys` 和 `oauth_clients` 表，或者让 API Key 表作为 `oauth_clients` 的一种特例（`grant_type=api_key`）。
- **逻辑层**:
  - 废弃数据库中对 Access Token 的持久化存储（Stateless JWT）。
  - 仅在需要“强制吊销”时使用 Redis 黑名单。

#### 第三阶段：SDK 化 (Developer Experience)

- 提供官方 SDK (Go/Node/Python)，封装 OAuth `client_credentials` 流程。
- 开发者只需配置 `ClientID` + `ClientSecret`，SDK 自动处理 Token 获取、缓存和刷新。
- **收益**: 彻底解决“OAuth 接入复杂”的痛点，同时保持高安全性。

---

## 3. 用户会话演进 (User Session / Human Auth)

目前的 Console Web 登录使用的是**完全无状态的单凭证 JWT (24小时过期)**，这在纯 Web SaaS 初期是足够轻量的。但在面向未来的多端客户端（Mobile App / Desktop App）以及高安全合规要求下，存在以下不足：

### 3.1 现状与隐患

1. **缺少 Refresh Token 机制**: 客户端（如手机 App）要求“一次登录，永久在线”。24小时硬过期会导致移动端频繁掉线，体验极差。
2. **缺乏会话强制注销 (Session Revocation)**: JWT 发出后无法撤回（除非改全局 Secret）。如果用户设备丢失或账号被盗，无法实现“踢人下线”或“登出所有设备”功能。
3. **缺乏设备级管理**: 后端不知道同一个账号目前在多少台设备上登录，无法做并发登录限制（如限制单个账号最多同时在 3 台设备登录）。

### 3.2 演进目标：状态化会话管理 (Stateful Session via OIDC/OAuth2)

为了支持未来的多端生态，用户鉴权需要向标准 OAuth2 授权码流程（或带有设备管理的 OIDC 扩展）演进：

1. **双 Token 机制 (Short-lived Access + Long-lived Refresh)**
   - 颁发短效 `access_token` (如 1 小时，完全无状态，用于高频 API 校验)。
   - 颁发长效 `refresh_token` (如 30 天，记录在数据库中，用于静默续期)。

2. **设备指纹与会话表 (Device & Session Registry)**
   - 建立 `user_sessions` 表，记录 `(user_id, device_id, refresh_token, last_active_ip, platform)`。
   - 提供 `/api/v1/users/me/sessions` 接口，允许用户在 Console 中查看所有活跃设备，并提供“注销指定设备”的入口。

3. **吊销机制 (Revocation via Redis)**
   - 当用户主动登出、修改密码或被管理员封禁时：
     1. 从 `user_sessions` 删除对应的 `refresh_token`。
     2. 将当前未过期的 `access_token` (或者其 `jti` 唯一标识) 存入 Redis 黑名单，直至其自然过期。
   - `JWTAuth` 中间件增加一层极轻量的 Redis 黑名单查验。

---

## 4. 计费与商业化支撑 (Billing & Commercialization)

鉴权统一后，计费逻辑将变得简单且集中。

### 3.1 计量架构 (Metering Architecture)

- **Meter Event**: 在统一鉴权中间件或业务 Service 结束时，发射“计量事件”。
- **Async Processing**: `UsageLog` -> `Message Queue` -> `Billing Service`。
- **SKU Design**: 引入 SKU 概念（如 `action_liveness.v1`），支持阶梯定价。

### 3.2 改造计划

1.  **Refactor Middleware**: 实施 `UnifiedAuthMiddleware`。
2.  **Simplify OAuth**: 移除 OAuth Token 数据库存储，改用纯 JWT 校验。
3.  **Enhance Metering**: 在统一鉴权后，添加统一的 `UsageMetering` 中间件，自动记录 SKU 使用量。

---

## 5. 行动清单 (Action Items)

- [ ] **Refactor**: 重构 `internal/middleware/oauth_client_auth.go`，统一 Context 注入逻辑。
- [ ] **Simplify**: 修改 `auth_handler.go`，移除 OAuth Token 落库逻辑，仅返回 JWT。
- [ ] **Doc**: 更新 API 文档，标记 API Key 为 Legacy（或仅限开发测试），推荐生产环境使用 OAuth + SDK。
- [ ] **Feat**: 设计 `BillingService` 接口，解耦计费逻辑。
- [ ] **Feat (User Auth)**: 规划 `user_sessions` 表结构，引入 Refresh Token 机制。
