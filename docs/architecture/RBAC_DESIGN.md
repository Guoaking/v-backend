# RBAC系统实现总结

## 概述
实现了完整的基于角色的访问控制（RBAC）系统，区分超级管理员（平台管理员）和组织管理员（组织所有者）的权限体系。

## 权限架构

### 1. 超级管理员 (Super Admin)
- **用户角色**: `role = 'admin'`
- **访问路径**: `/api/v1/admin/*`
- **权限范围**: 全平台管理
- **功能**:
  - 查看所有用户列表（包含组织和使用数据）
  - 管理用户状态（启用/禁用）
  - 查看所有组织列表（包含成员数量和使用数据）
  - 查看全平台审计日志（包含用户名称关联）

### 2. 组织管理员 (Organization Admin)
- **组织角色**: `org_role = 'owner'`
- **访问路径**: `/api/v1/orgs/*`
- **权限范围**: 单租户组织管理
- **功能**:
  - 查看当前组织信息
  - 查看组织成员列表
  - 邀请新成员加入组织
  - 更新组织套餐

### 3. 组织编辑者 (Organization Editor)
- **组织角色**: `org_role = 'editor'`
- **访问路径**: `/api/v1/orgs/*`（部分功能受限）
- **权限范围**: 组织信息查看（只读）
- **功能**:
  - 查看当前组织信息
  - 查看组织成员列表
  - **不能**: 邀请成员、更新套餐

## 技术实现

### JWT Claims结构
```json
{
  "userID": "用户ID",
  "email": "用户邮箱", 
  "role": "admin|user",           // 用户角色
  "orgID": "组织ID",
  "orgRole": "owner|editor|viewer" // 组织角色
}
```

### RBAC中间件

#### 1. RequireRole - 用户角色检查
```go
func RequireRole(requiredRole string) gin.HandlerFunc
```
检查JWT中的`role`字段，用于超级管理员权限验证。

#### 2. RequireOrgRole - 组织角色检查  
```go
func RequireOrgRole(requiredOrgRole string) gin.HandlerFunc
```
检查JWT中的`orgRole`字段，用于组织管理员权限验证。

#### 3. RequireRoleOrOrgRole - 组合权限检查
```go
func RequireRoleOrOrgRole(requiredRole string, requiredOrgRole string) gin.HandlerFunc
```
同时检查用户角色和组织角色，提供灵活的权限控制。

### API端点设计

#### 超级管理员端点 (`/api/v1/admin/*`)
- `GET /admin/users` - 获取用户列表（包含组织和使用数据）
- `PUT /admin/users/:id/status` - 更新用户状态
- `GET /admin/organizations` - 获取组织列表（包含成员数量和使用数据）
- `GET /admin/audit-logs` - 获取审计日志（包含用户名称关联）

#### 组织管理端点 (`/api/v1/orgs/*`)
- `GET /orgs/current` - 获取当前组织信息（owner/editor可访问）
- `GET /orgs/members` - 获取组织成员列表（owner/editor可访问）
- `POST /orgs/invite` - 邀请新成员（仅owner可访问）
- `PUT /orgs/plan` - 更新组织套餐（仅owner可访问）

### 数据库设计

#### 核心表结构
1. **users** - 用户表（包含role和org_role字段）
2. **organizations** - 组织表
3. **audit_logs** - 审计日志表
4. **organization_invitations** - 组织邀请表
5. **usage_metrics** - 使用统计数据表

#### 关键SQL查询

**用户列表查询（超级管理员）**:
```sql
SELECT u.id, u.full_name, u.email, u.role, u.status, u.avatar_url,
       o.name as org_name, o.plan_id, u.created_at,
       COALESCE(SUM(um.request_count), 0) as total_usage
FROM users u
LEFT JOIN organizations o ON u.org_id = o.id  
LEFT JOIN usage_metrics um ON um.org_id = o.id
GROUP BY u.id, u.full_name, u.email, u.role, u.status, u.avatar_url, o.name, o.plan_id, u.created_at
```

**组织列表查询（超级管理员）**:
```sql
SELECT o.id, o.name, o.plan_id, o.billing_email, o.status, o.created_at,
       COUNT(u.id) as member_count,
       COALESCE(SUM(um.request_count), 0) as total_usage
FROM organizations o
LEFT JOIN users u ON u.org_id = o.id AND u.status = 'active'
LEFT JOIN usage_metrics um ON um.org_id = o.id
GROUP BY o.id, o.name, o.plan_id, o.billing_email, o.status, o.created_at
```

### 安全特性

1. **JWT令牌验证** - 所有管理端点都需要有效的JWT令牌
2. **角色权限检查** - 多层级的角色权限验证
3. **组织隔离** - 组织数据严格按org_id隔离
4. **审计日志** - 所有管理操作都记录审计日志
5. **错误处理** - 统一的错误响应格式和状态码

### 性能优化

1. **数据库索引** - 在关键字段上建立索引（user_id, org_id, email等）
2. **查询优化** - 使用JOIN和GROUP BY优化聚合查询
3. **缓存策略** - 支持Redis缓存（已集成）
4. **分页支持** - 列表接口支持分页参数

## 测试验证

使用提供的`test_rbac.sh`脚本可以验证：
1. 超级管理员可以访问所有管理端点
2. 组织管理员可以访问组织管理端点
3. 普通用户只能访问部分组织信息
4. 权限不足时会返回403错误

## 部署说明

1. 确保数据库表结构已创建（包含新的organization_invitations表）
2. 配置JWT密钥和环境变量
3. 启动服务后使用测试脚本验证RBAC功能
4. 监控审计日志确保权限系统正常工作