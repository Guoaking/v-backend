# Backend Integration Testing Guide

This guide describes how to efficiently write and maintain Integration tests for the KYC backend.

## 1. Overview
Integration tests verify the entire stack (API -> Middleware -> Service -> Database/Cache) by simulating real HTTP requests using `httptest`.

- **Location**: `tests/integration/`
- **Framework**: `testify/suite` + `RequestBuilder` (Fluent API)
- **Database**: Uses `config.local.yaml` (usually a local Postgres/Redis).

## 2. Test File Structure

The Integration tests are modularized by business domain:
- [context.go](file:///v-backend/tests/integration/context.go): Shared test suite base and helpers.
- [kyc_core_test.go](file:///v-backend/tests/integration/kyc_core_test.go): OCR, Face, and Liveness core business.
- [auth_security_test.go](file:///v-backend/tests/integration/auth_security_test.go): Login, Register, RBAC, and STS.
- [console_mgmt_test.go](file:///v-backend/tests/integration/console_mgmt_test.go): User Profile, Usage, Quota, and OAuth Clients.
- [admin_org_test.go](file:///v-backend/tests/integration/admin_org_test.go): Organization and Admin management.

## 3. Identity Injection
The `RequestBuilder` supports multiple identities:

| Method | Role | Context |
|--------|------|---------|
| `.AsUser()` | Console User | Has `user_id` and `org_id` |
| `.AsAdmin()` | Platform Admin | Has `IsPlatformAdmin: true` |
| `.AsApp(scopes...)` | Third-party App | OAuth Client Credentials (no `user_id`) |
| `.AsPlayground()` | Sandbox User | STS Token (both `user_id` and `org_id`) |

## 4. Common Assertions

- `ExpectSuccess()`: Assert `200 OK`.
- `ExpectStatus(code)`: Assert a specific HTTP status (e.g., `201 Created`).
- `ExpectForbidden()`: Assert `403 Forbidden`.
- `ExpectUnauthorized()`: Assert `401 Unauthorized`.
- `ExpectErrorCode(bizCode)`: Assert a business-level error code in the JSON body.
- `ExpectJSON(target)`: Assert `200 OK` and unmarshal body into `target`.

## 5. Side-Effect Verification
Use the `BaseSuite` helpers to check the database:

```go
// Verify that a usage log was generated for billing
s.VerifyUsageLogged("ocr")
```

## 6. Multipart (File Upload)
For OCR or Face Search tests:

```go
files := map[string][]byte{"picture": []byte("content")}
fields := map[string]string{"type": "id_card"}
body, contentType := s.Ctx.MultipartBody(files, fields)

s.Ctx.NewRequest(s.T(), "POST", "/api/v1/kyc/ocr").
    AsApp().
    WithBody(body).
    WithHeader("Content-Type", contentType).
    ExpectSuccess()
```

## 7. Running Tests

### 7.1 Run All Tests
```bash
cd v-backend && go test -v ./tests/e2e/...
```

### 7.2 Run Smoke Tests (Quick Check)
Tests prefixed with `TestSmoke_` are critical path tests.
```bash
go test -v -run Smoke ./tests/e2e/...
```

### 7.3 Run Targeted Suite
```bash
go test -v -run KYCCoreSuite ./tests/e2e/...
```

## 8. Best Practices
1. **Clean Slate**: Use `s.seedData()` in `BaseSuite` to ensure consistent state.
2. **Reverse Testing**: Always add a test case for `.AsUser().ExpectForbidden()` when testing admin-only APIs.
3. **SSOT**: Reference `v-backend/AGENTS.md` for the latest documentation standards.

## 9. E2E Coverage Checklist

This checklist tracks the E2E coverage of backend routes.

### 9.1 KYC APIs (High Priority)
- [x] `POST /api/v1/kyc/ocr` (OCR Recognition)
- [x] `POST /api/v1/kyc/face/search` (Face Search)
- [x] `POST /api/v1/kyc/face/compare` (Face Compare)
- [x] `POST /api/v1/kyc/face/id-match` (Face ID Match)
- [x] `POST /api/v1/kyc/face/detect` (Face Detect)
- [x] `POST /api/v1/kyc/liveness/silent` (Silent Liveness)
- [x] `POST /api/v1/kyc/liveness/video` (Video Liveness)
- [x] `POST /api/v1/kyc/liveness/action/session` (Action Liveness Session)
- [x] `POST /api/v1/kyc/liveness/action/upload` (Action Liveness Upload)
- [x] `POST /api/v1/kyc/liveness/action/verify` (Action Liveness Verify)
- [x] `POST /api/v1/kyc/verify` (Complete KYC)
- [x] `GET /api/v1/kyc/status/:request_id` (KYC Status)

### 9.2 Image & Face Retrieval
- [x] `GET /api/v1/faces/:id/image` (Face Image Retrieval)
- [x] `GET /api/v1/images/:id/image` (General Image Retrieval)
- [x] `POST /api/v1/images` (Image Upload)

### 9.3 Console APIs
- [x] `GET /api/v1/console/users/me` (Get Current User)
- [x] `PUT /api/v1/console/users/me` (Update Profile)
- [x] `POST /api/v1/console/sandbox/token` (Generate STS Token)
- [x] `GET /api/v1/console/usage` (Get Usage Logs)
- [x] `GET /api/v1/console/usage/stats` (Get Usage Stats)
- [x] `GET /api/v1/console/usage/quota` (Get Quota Status)
- [x] `GET /api/v1/console/oauth/clients` (List OAuth Clients)
- [x] `POST /api/v1/console/oauth/clients/register` (Register Client)
- [x] `PUT /api/v1/console/oauth/clients/:id` (Update Client)
- [x] `DELETE /api/v1/console/oauth/clients/:id` (Delete Client)
- [x] `POST /api/v1/console/oauth/clients/:id/rotate` (Rotate Secret)
- [ ] `GET /api/v1/console/oauth/clients/:id/secret` (Get Secret)
- [ ] `GET /api/v1/console/me/notifications` (List Notifications)

### 9.4 Organization Management
- [x] `POST /api/v1/orgs` (Create Organization)
- [x] `POST /api/v1/orgs/switch` (Switch Organization)
- [x] `GET /api/v1/orgs/current` (Get Current Org)
- [x] `GET /api/v1/orgs/members` (List Members)
- [x] `POST /api/v1/orgs/members` (Invite Member)
- [ ] `PATCH /api/v1/orgs/members/:id` (Update Role/Status)
- [ ] `GET /api/v1/orgs/billing` (Get Billing)
- [ ] `GET /api/v1/orgs/usage/detailed` (Detailed Usage)
- [ ] `GET /api/v1/orgs/audit-logs` (Org Audit Logs)

### 9.5 Admin APIs
- [x] `GET /api/v1/admin/stats/overview` (Overview Stats)
- [x] `GET /api/v1/admin/users` (List Users)
- [x] `GET /api/v1/admin/organizations` (List Organizations)
- [x] `GET /api/v1/admin/audit-logs` (Global Audit Logs)
- [x] `PUT /api/v1/admin/users/:id/status` (Update User Status)
- [ ] `PUT /api/v1/admin/organizations/:id/plan` (Update Org Plan)
- [x] `POST /api/v1/admin/organizations/:id/quotas/adjust` (Adjust Quota)
- [ ] `POST /api/v1/admin/permissions` (Manage Permissions)

### 9.6 Auth & Extra
- [x] `POST /api/v1/auth/login` (Console Login)
- [x] `POST /api/v1/auth/register` (Console Register)
- [x] `GET /api/v1/meta/permissions` (List Permissions)
- [x] `GET /api/v1/meta/roles` (List Roles)
- [x] `POST /api/v1/oauth/token` (Get OAuth Token)
- [ ] `POST /api/v1/auth/password-reset/request` (PW Reset Request)
- [ ] `POST /api/v1/auth/password-reset/confirm` (PW Reset Confirm)
