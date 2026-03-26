# KYC Enterprise Authentication Service - Agent Context

> **CRITICAL INSTRUCTION**: This is the source of truth for architecture and constraints.
>
> 1. **Check `docs/BACKEND_GUIDE.md`** for coding standards before writing any code.
> 2. **Check `docs/architecture/AUTH_UNIFICATION.md`** before touching auth logic.
> 3. **DO NOT** rely on legacy patterns found in old handlers.

## 1. Documentation Index

- **Standards**: [docs/BACKEND_GUIDE.md](docs/BACKEND_GUIDE.md) (Checklist, Error Codes, API Contract)
- **Knowledge Base**: [docs/kb/AI_KNOWLEDGE_BASE.md](docs/kb/AI_KNOWLEDGE_BASE.md) (Runbooks, Commands)
- **Architecture**:
  - [Auth Unification](docs/architecture/AUTH_UNIFICATION.md)
  - [Action Liveness Spec](docs/specs/action_liveness_backend_spec.md)

### Documentation Maintenance (Gardening)

- **Index It or Lose It**: Every new markdown file MUST be indexed here or in `docs/BACKEND_GUIDE.md`. Unindexed files are considered "dead code".
- **Prune relentlessly**: After major features or refactors, review existing docs.
  - **Delete** outdated files.
  - **Merge** fragmented notes into Guides.
  - **Clarify** ambiguous sections.
- **Objective Truth**: Docs should reflect the _current_ state of code.

## 2. Code Map (Where things live)

| Component      | Path                                          | Description                                                        |
| -------------- | --------------------------------------------- | ------------------------------------------------------------------ |
| **Entry**      | `cmd/server/main.go`                          | App initialization, middleware chain                               |
| **Auth**       | `internal/api/auth_handler.go`                | **MODERN** Unified OAuth2/STS (Token, Client Credentials).       |
| **KYC Logic**  | `internal/service/kyc_service.go`             | Core orchestration (OCR, Face)                                     |
| **Liveness**   | `internal/service/action_liveness_service.go` | **MODERN** implementation reference (Quota, Audit, Tracing)        |
| **Middleware** | `internal/middleware/`                        | `quota.go`, `security.go`, `oauth_client_auth.go`                  |
| **Models**     | `internal/models/`                            | GORM structs. **Note**: `KYCRequest` is a shared monolithic table. |

## 3. Architecture Highlights

### Authentication (Unified)

- **Current State**: Unified OAuth2/STS (Security Token Service).
- **Note**: Legacy API Keys have been safely removed from business logic.
- **Rule**: All public APIs MUST use `APIOrOAuthAuth` middleware. Use `STS` for short-lived (15 min) Playground access.
- **Reference**: See `docs/architecture/AUTH_UNIFICATION.md`.

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
- **Run**: `make run` (Uses `config.local.yaml` by default)
- **Build**: `make build`
- **Docs**: `swag init -g cmd/server/main.go -o docs` (Update Swagger)

---

## 6. Golden Rules

1.  **Verify**: Run `./scripts/test-quick.sh` before finishing.
2.  **Sync**: Update frontend `types.ts` if API changes.
3.  **Style**: Mimic existing Go patterns (e.g., `if err != nil`).
4.  **Deps**: No new Go modules without strong justification.

_End of Context_
