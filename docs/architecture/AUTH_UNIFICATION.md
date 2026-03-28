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

#### 第一阶段：中间件统一 (The Gatekeeper) - **已完成**

重构 `APIOrOAuthAuth` 中间件，使其成为统一的鉴权入口。

#### 第二阶段：模型融合 (Model Convergence) - **进行中**

- **数据层**: 考虑合并 `api_keys` 和 `oauth_clients` 表。
- **逻辑层**: 已实现 STS (Security Token Service) 端点，Playground 已全面切到短效 JWT。

#### 第三阶段：SDK 化 (Developer Experience) - **计划中**

- 提供官方 SDK (Go/Node/Python)，封装 OAuth `client_credentials` 流程。
- 开发者只需配置 `ClientID` + `ClientSecret`，SDK 自动处理 Token 获取、缓存和刷新。
- **收益**: 彻底解决“OAuth 接入复杂”的痛点，同时保持高安全性。

---

## 3. 用户会话演进 (User Session / Human Auth)

目前的 Console Web 登录使用的是**完全无状态的单凭证 JWT (24小时过期)**，这在纯 Web SaaS 初期是足够轻量的。但在面向未来的多端客户端（Mobile App / Desktop App）以及高安全合规要求下，存在以下不足：

### 3.1 现状与隐患 (Current Implementation & Flaws)

在目前的 `v-frontend` 和 `v-backend` 中，采用的是**完全无状态的单凭证 JWT**：

- **存储机制**: 前端在 `AuthContext.tsx` 中拿到 Token，直接存入 `localStorage.setItem('vl_token', token)`，并在 `apiClient.ts` 中拦截放入 `Authorization` Header。
- **过期处理**: JWT 24小时硬过期。过期后前端捕获 401 触发 `logout()` 强制踢回登录页。

**这种极简模式在纯 Web SaaS 初期是足够轻量的，但存在严重隐患：**

1. **XSS 攻击风险**: `localStorage` 极易受到跨站脚本攻击，一旦恶意脚本获取到 JWT，等同于接管了账号。
2. **缺少 Refresh Token 机制**: 客户端（如手机 App）要求“一次登录，永久在线”。24小时硬过期会导致移动端频繁掉线，体验极差。
3. **缺乏会话强制注销 (Session Revocation)**: JWT 发出后无法撤回（除非改全局 Secret）。如果用户设备丢失或账号被盗，无法实现“踢人下线”或“登出所有设备”功能。
4. **缺乏设备级管理**: 后端不知道同一个账号目前在多少台设备上登录，无法做并发登录限制（如限制单个账号最多同时在 3 台设备登录）。

### 3.2 演进目标：状态化会话管理 (Stateful Session via OIDC/OAuth2)

为了支持未来的多端生态和极高的安全性，用户鉴权需要向标准 OAuth2 授权码流程（或带有设备管理的 OIDC 扩展）演进。主流 B2B SaaS (如 Stripe, AWS, Vercel) 通常采用**混合双 Token 机制**：

1. **双 Token 机制 (Short-lived Access + Long-lived Refresh)**
   - 颁发短效 `access_token` (如 15分钟 - 1小时，完全无状态，用于高频 API 校验)。
   - 颁发长效 `refresh_token` (如 30 天，记录在数据库中，用于静默续期)。

2. **存储策略演进 (Storage Strategy)**
   - **Access Token**: 存在前端内存 (JS 变量) 或 `localStorage` 中（寿命短，风险可控）。
   - **Refresh Token**: **必须存放在 `HttpOnly` + `Secure` 的 Cookie 中**，绝对免疫 XSS 攻击。前端无法读取，但在发送 `/api/v1/auth/refresh` 请求时浏览器会自动带上。

3. **前端静默续期 (Silent Refresh)**
   - 改造 `apiClient.ts` 的响应拦截器 (Response Interceptor)。
   - 当收到 `401 Unauthorized` 报错时，不要立刻 `logout`。
   - 挂起当前请求，静默调用一次 `POST /api/v1/auth/refresh`。
   - 如果续期成功，拿到新 Token 替换旧 Token，然后重试刚才失败的请求；如果续期失败，才真正踢回登录页。

4. **设备指纹与会话表 (Device & Session Registry) - 已通过 Redis 实现**
   - 摒弃了厚重的关系型数据库 `user_sessions` 表。
   - 改为在 Redis 中存储结构化的 Session 字典：`session:{user_id}:{jti} -> {user_agent, ip, last_seen}`。
   - 提供 `/api/v1/console/users/me/sessions` 接口，允许用户在 Console 中查看所有活跃设备。

5. **吊销与踢人机制 (Revocation & Kick-out) - 已实现**
   - 提供了 `/api/v1/console/users/me/sessions/:id` (DELETE) 接口。
   - 当管理员点击“强制下线”或用户主动登出时：
     1. 从 Redis 的 Active Sessions (`session:{user_id}:{jti}`) 中删除该设备。
     2. 将该 `jti` 加入黑名单 (`blocklist:{jti} = "revoked"`)，保留时间与 JWT 原本过期时间一致。
   - `JWTAuth` 中间件增加一层极轻量的 Redis 黑名单查验。如果当前请求携带的 JWT 的 `jti` 在黑名单中，立刻返回 401。

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
- [x] **Feat (User Auth)**: 通过 Redis 引入 Active Sessions 机制和基于 `jti` 的 Blocklist。
- [ ] **Feat (User Auth)**: 引入 HttpOnly Cookie 存放长效 Refresh Token 机制。
