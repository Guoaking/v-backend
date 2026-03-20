# Playground 鉴权演进与控制台设计方案

## 1. 背景与目标

为了在未来彻底废弃长期有效的 `API Key`，全面转向更安全的 `OAuth 2.0 Client Credentials` 和 `短效 JWT` 机制，我们需要解决一个核心痛点：**如果不允许在前端直接使用 API Key，那么 Web 控制台中的 Playground（测试沙盒）该如何发起真实合法的 API 请求？**

本方案旨在结合主流 B2B SaaS（如 Stripe, AWS, Vercel）的最佳实践，设计一套基于**系统级内置 OAuth Client** 的隐式鉴权机制，并规范相关敏感资产的权限可见性。

**本文档与 `AUTH_UNIFICATION.md` 的关系：**

- `AUTH_UNIFICATION.md` 描述的是**后端网关层**的宏观演进（废弃 API Key 库查询，全面拥抱无状态 Machine JWT）。
- 本文档 (`PLAYGROUND_AUTH_EVOLUTION.md`) 是统一鉴权计划在**前端 Web 控制台与第三方 H5 场景下**的工程落地补充方案，解决的是“在没有长期 API Key 后，前端如何合法获取临时 JWT”的问题。

---

## 2. 核心架构：系统内置的特殊 Client

### 2.1 什么是系统内置 Client？

在系统数据库的 `oauth_clients` 表中，初始化一个全局唯一的记录：

- `client_id`: `sys_web_console_playground`
- `client_secret`: `[仅后端环境变量持有，绝不落盘到前端]`
- `is_system`: `true`

这个 Client 属于**全局系统维度**，不属于任何具体的租户（Organization）或用户（User）。

### 2.2 鉴权与计费流转 (STS 机制)

当一个用户（例如张三，属于 Org A）在浏览器中登录并进入 Playground 页面点击“运行测试”时，发生以下流程：

1. **前端请求 Token**：前端带着张三的登录态（User Session Cookie / JWT），请求后端的一个特殊接口 `POST /api/v1/console/sandbox/token`。
2. **后端 STS (Security Token Service) 签发**：
   - 后端校验张三的登录态合法。
   - 后端读取环境变量中的 `sys_web_console_playground` Secret。
   - 后端生成一个**短效的 Machine JWT (如 15 分钟)**，并在 JWT 的 Claims 中强制注入上下文：
     ```json
     {
       "client_id": "sys_web_console_playground",
       "org_id": "Org_A", // 继承张三当前所在的组织
       "user_id": "User_Zhang3", // 记录操作人，用于审计
       "source": "playground",
       "exp": 1712345678
     }
     ```
3. **前端消费 Token**：前端拿到这个短效 JWT 后，放入 `Authorization: Bearer <Token>` 请求真正的 AI 接口。
4. **统一计费与限流**：底层的 AI 网关/中间件解析 JWT，看到 `org_id = Org_A`，直接从 Org A 的配额池中扣除额度，实现了 Playground 用量与 API 用量的统一计费。

### 2.3 第三方 App 内嵌 H5 场景 (3rd-Party Embed)

如果 Playground 页面是被第三方 App（如某银行 App）通过 WebView 嵌入的，此时用户并没有 Verilocale 的控制台登录态。

**流程设计：STS 下放机制**

1. **获取 Token**：第三方 App 的后端使用他们自己在 `/console/oauth` 创建的 `Client ID` 和 `Client Secret`，调用 Verilocale 的 `POST /oauth/token` 获取一个正式的、代表该银行的 Machine JWT。
2. **注入 Token**：第三方后端将该 Token 拼接到 H5 URL 中下发给前端（如 `?embed=true&token=eyJhbGci...`）。
3. **消费 Token**：前端 H5 页面识别到 URL 中的 `token` 参数后，**不再向 `sys_web_console_playground` 申请**，而是直接使用这个注入的 Token 发起 AI 请求。
4. **统一计费**：底层 AI 网关解析 JWT 发现其属于“某银行 Org”，直接扣除该银行的额度。

### 2.4 为什么这么设计？

- **极致安全**：前端永远拿不到 Client Secret，且拿到的 Token 15 分钟即废弃，即使泄露也无法造成大面积损失。
- **架构解耦**：底层 AI 接口只需要认 JWT 和 OrgID，彻底干掉了为了 Playground 而保留 API Key 的妥协。
- **无需配额孤岛**：不需要给系统 Client 单独设额度，计费直接挂载到当前登录用户的组织上。

---

## 3. 控制台敏感资产管理 (OAuth Apps / API Keys 页面设计)

这属于典型的**多租户架构下的“资源所有权与可见性 (Resource Ownership & Visibility)”问题**。

### 3.1 页面可见性与操作权限 (RBAC 映射)

不应该对普通成员直接隐藏左侧导航栏的 `/console/oauth` 菜单，而应该采用**降级呈现**的策略，让用户清晰知道自己的权限边界。

| 角色 (Org Role)        | 列表可见范围                 | 可执行操作 (Create/Rotate/Delete)  | UI 呈现策略                                                                                                   |
| :--------------------- | :--------------------------- | :--------------------------------- | :------------------------------------------------------------------------------------------------------------ |
| **Owner / Admin**      | 组织内 **全部** Clients      | 可以管理 **所有人** 创建的 Clients | 完整列表，需包含 `Created By` 列以便审计溯源。                                                                |
| **Developer / Editor** | 仅限 **自己创建** 的 Clients | 只能管理 **自己创建** 的 Clients   | 过滤后的列表。顶部提示：“您只能管理由您创建的凭证。如需查看全局凭证，请联系管理员。”                          |
| **Viewer / Member**    | **不可见** (空状态)          | **无权限**                         | 页面中间展示锁图标 (Empty State)。提示：“您的权限级别 (Viewer) 无法访问 API 凭证。请联系管理员进行系统集成。” |

### 3.2 敏感数据展示原则 (Secret Display Policy)

针对 `Client Secret` 或 `API Key` 的明文展示，必须遵循严格的业界标准：

1. **Create-Once Display (仅在创建时展示一次)**：
   - 无论是 Owner 还是 Developer，当点击“创建”按钮后，生成的 Secret 只能在弹窗中明文展示**一次**。
   - 弹窗关闭后，列表中永远只展示打码版本（如 `sk-••••••••••••1234`）。
2. **强制轮换 (Force Rotation)**：
   - 如果用户忘记了 Secret，不提供“查看”功能，只提供“Rotate (轮换/重置)”功能。
   - 轮换后旧的 Secret 立即（或 24 小时后）失效，展示新的 Secret 给用户复制。
3. **系统级 Client 隔离**：
   - 所有获取 Client 列表的 API (`GET /console/oauth/clients`) 必须强制附加 `WHERE is_system = false`。
   - 确保前文提到的 `sys_web_console_playground` 永远不会出现在任何用户的控制台界面中，防止用户误删或试图复制其 Secret。

---

## 4. 工程技术落地方案 (Implementation Roadmap)

为了从现状平滑过渡到上述架构，我们需要分两端进行改造：

### 4.1 后端改造点 (v-backend)

1. **DB Seed**: 数据库初始化脚本中插入 `is_system=true` 的 `sys_web_console_playground` 记录。
2. **STS 端点开发**: 新增 `POST /api/v1/console/sandbox/token` 接口。
   - 依赖现有的 `JWTAuth` 中间件确保调用者已登录。
   - 根据调用者的 `OrgID`，使用系统 Client 的凭证动态签发包含此 `OrgID` 的 15 分钟短效 JWT。
3. **列表 API 过滤**: 修改 `GetOAuthClients` Handler，增加 `is_system = false` 过滤条件。
4. **RBAC 拦截**: 修改 API 凭证相关的 Handler，如果是 Viewer 角色直接返回 403；如果是 Editor 则只返回/操作 `created_by == currentUser.ID` 的记录。

### 4.2 前端改造点 (v-frontend)

1. **移除 Key 选择框**: 移除 Playground 左上角的 API Key 下拉选择器组件 (`CustomSelect`)。
2. **智能鉴权分发逻辑 (Playground.tsx)**:
   - 如果是 `isH5Mode` 且 URL 中存在 `token`，直接使用该 Token 初始化 `ApiClient` (支持第三方集成)。
   - 如果是正常的 Web 模式或扫描生成的 Demo 二维码（无外部 Token），在进入页面时自动调用后端的 STS 端点换取短效 JWT。
3. **代码生成器更新**: 右侧的 Code Snippet 不再展示具体的 API Key，而是展示示例代码，引导用户使用正式的 Client ID/Secret 进行初始化。
4. **权限降级 UI (OAuth/API Keys 页面)**:
   - 引入 `PermissionContext` 校验。
   - 针对 Viewer 展示“锁定/无权限”的 Empty State。
   - 实现 Secret 的“Create-Once Display”弹窗逻辑，列表仅渲染 `mask_secret`。
