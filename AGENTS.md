# KYC Enterprise Authentication Service - Agent Context

> **CRITICAL INSTRUCTION**: This is the source of truth for architecture and constraints.
>
> 1. **Check `BACKEND_GUIDE.md`** for coding standards before writing any code.
> 2. **Check `docs/AUTH_UNIFICATION_PLAN.md`** before touching auth logic.
> 3. **DO NOT** rely on legacy patterns found in old handlers.

## 1. Documentation Index

- **Standards**: [BACKEND_GUIDE.md](BACKEND_GUIDE.md) (Checklist, Error Codes, API Contract)
- **Knowledge Base**: [docs/AI_FIRST_BACKEND_KB.md](docs/AI_FIRST_BACKEND_KB.md) (Runbooks, Commands)
- **Architecture**:
  - [STS / Token Service](docs/STS_ARCHITECTURE.md)
  - [Auth Unification](docs/AUTH_UNIFICATION_PLAN.md)

## 2. Code Map (Where things live)

| Component      | Path                                          | Description                                                        |
| -------------- | --------------------------------------------- | ------------------------------------------------------------------ |
| **Entry**      | `cmd/server/main.go`                          | App initialization, middleware chain                               |
| **Auth**       | `internal/api/auth_handler.go`                | **LEGACY** OAuth2. Moving to Unified Middleware.                   |
| **KYC Logic**  | `internal/service/kyc_service.go`             | Core orchestration (OCR, Face)                                     |
| **Liveness**   | `internal/service/action_liveness_service.go` | **MODERN** implementation reference (Quota, Audit, Tracing)        |
| **Middleware** | `internal/middleware/`                        | `quota.go`, `security.go`, `oauth_client_auth.go`                  |
| **Models**     | `internal/models/`                            | GORM structs. **Note**: `KYCRequest` is a shared monolithic table. |

## 3. Architecture Highlights

### Authentication (Transitioning)

- **Current State**: Mixed (API Key + OAuth2 Client Credentials).
- **Target State**: Unified Middleware handling both.
- **Rule**: When adding new endpoints, use `UnifiedAuthMiddleware` (once available) or follow `docs/AUTH_UNIFICATION_PLAN.md`.

### Billing & Quota

- **Mechanism**: `checkAndConsumeQuota` (Redis + DB fallback).
- **Enforcement**: MUST be called for any cost-incurring operation.
- **Reference**: See `UploadVideo` in `action_liveness_service.go`.

### Observability

- **Tracing**: OpenTelemetry (`tracing.StartSpan`). **MANDATORY** for all public API handlers.
- **Metrics**: Prometheus. Use `metrics.RecordBusinessOperation`.
- **Audit**: `models.AuditLog`. Critical for security actions.

## 4. Development Constraints

1.  **No `map[string]any`**: Always define strict structs for API responses.
2.  **No Mocking in Prod Code**: Remove any `if Config.Mock` logic in production paths. Use interfaces for testing.
3.  **Secrets**: Never log secrets. Use `Masked` fields in responses.
4.  **Error Handling**: Use `Code*` constants (e.g., `CodeBusinessError`), never magic numbers.

## 5. Quick Commands (For Agent Execution)

- **Test**: `./scripts/test-quick.sh` (Fast unit tests)
- **Lint**: `go vet ./...`
- **Run**: `go run cmd/server/main.go -config config.local`
- **Docs**: `swag init -g cmd/server/main.go -o docs` (Update Swagger)

---

_End of Context_
