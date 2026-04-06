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
  - [Attendance App Design](docs/architecture/ATTENDANCE_APP_DESIGN.md)
  - [Action Liveness Spec](docs/specs/action_liveness_backend_spec.md)
  - [Attendance App Spec](docs/specs/attendance_app_spec.md)
  - [Attendance P00 Blueprint](docs/specs/attendance_p00_blueprint.md)

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
| **Auth**       | `internal/api/auth_handler.go`                | Unified OAuth2/STS (Token, Client Credentials)                     |
| **Attendance** | `internal/apps/attendance/`                   | Attendance pilot BFF, employee punch flows, and console endpoints |
| **KYC Logic**  | `internal/service/kyc_service.go`             | Core orchestration (OCR, Face)                                     |
| **Liveness**   | `internal/service/action_liveness_service.go` | Reference implementation for quota, audit, and tracing             |
| **Storage**    | `internal/storage/storage_service.go`         | Policy-based Storage Resolver with Nginx Internal Redirect         |
| **Testing**    | `tests/integration/`                          | [docs/guides/INTEGRATION_TESTING_GUIDE.md](docs/guides/INTEGRATION_TESTING_GUIDE.md) |
| **Middleware** | `internal/middleware/`                        | `quota.go`, `security.go`, `oauth_client_auth.go`                  |
| **Models**     | `internal/models/`                            | GORM structs. **Note**: `KYCRequest` is a shared monolithic table. |

## 3. Architecture Highlights

### Storage & Serving (Unified)

- **Mechanism**: `StorageService` resolves paths based on `AccessRule` and `UploadRule` chains.
- **Serving**: Supports **Nginx Internal Redirect** (`X-Accel-Redirect`) for production and **Smart Streaming** for local development.
- **Rules**: Mapped by feature prefix (e.g., `faces/` -> `/data/dataset/`).

### Authentication (Unified)

- **Current State**: Unified OAuth2/STS (Security Token Service).
- **Note**: Legacy API Keys have been safely removed from business logic.
- **Rule**: All public APIs MUST use `APIOrOAuthAuth` middleware. Use `STS` for short-lived (15 min) Playground access.
- **Reference**: See [docs/architecture/STS_AND_PLAYGROUND_AUTH.md](../docs/architecture/STS_AND_PLAYGROUND_AUTH.md) and `docs/architecture/AUTH_UNIFICATION.md`.

### Billing & Quota

- **Mechanism**: `checkAndConsumeQuota` (Redis + DB fallback).
- **Enforcement**: MUST be called for any cost-incurring operation.
- **Reference**: See `UploadVideo` in `action_liveness_service.go`.

### Observability

- **Tracing**: OpenTelemetry (`tracing.StartSpan`). **MANDATORY** for all public API handlers.
- **Metrics**: Prometheus. Use `metrics.RecordBusinessOperation`.
- **Audit**: `models.AuditLog`. Critical for security actions.

### API Compatibility

- **Contract Mindset**: Public JSON shape, error codes, auth headers, callback payloads, and file upload semantics are versioned contracts.
- **Breaking Changes**: If an API or callback contract changes, update Swagger/spec docs, frontend consumer types, integration tests, and migration notes in the same change.
- **Deprecation Rule**: Preserve old behavior during the migration window unless removal is explicitly documented.

### Data Governance

- **Separation of Concerns**: Keep business facts, audit logs, and algorithm sample data logically separate even if they currently live in the same service.
- **Sensitive Data**: Face images, OCR raw data, fallback captures, and tokens must have controlled access paths, retention expectations, and deletion behavior documented.
- **Client Storage Awareness**: If backend behavior causes sensitive identifiers to be cached on the client, document the risk and avoid increasing scope casually.

### Operational Safety

- **Feature Promotion**: Pilot modules must not be treated as general-purpose product capability until auth boundaries, admin permissions, monitoring, and support runbooks are clear.
- **Runbook Trigger**: New async jobs, callbacks, retry flows, or quota paths should add or update a runbook/reference document.
- **Rollback Readiness**: If a change affects auth, billing, storage, or attendance flows, document how to disable or roll back the path safely.

## 4. Development Constraints

1.  **No `map[string]any`**: Always define strict structs for API responses.
2.  **No Mocking in Prod Code**: Remove any `if Config.Mock` logic in production paths. Use interfaces for testing.
3.  **Secrets**: Never log secrets. Use `Masked` fields in responses.
4.  **Error Handling**: Use `Code*` constants (e.g., `CodeBusinessError`), never magic numbers.
5.  **Path Resolution**: NEVER hardcode `/data` or `/mnt`. ALWAYS use `StorageService.ResolveAccess`.
6.  **Boundary Clarity**: Do not let product BFF layers silently bypass core auth, quota, or audit policy without documenting the ownership model.
7.  **Explicit Migration**: Schema or route changes that impact existing customers require a migration note or compatibility explanation.

## 5. Quick Commands (For Agent Execution)

- **Test**: `./scripts/test-quick.sh` (Fast unit tests)
- **Lint**: `go vet ./...`
- **Format**: `go fmt ./...` (CRITICAL before commit)
- **Run**: `make run` (Uses `config.local.yaml` by default)
- **Build**: `make build`
- **Docs**: `swag init -g cmd/server/main.go -o docs` (Update Swagger)

---

## 6. Golden Rules

1.  **Verify (DoD)**:
    - Logic changes: run unit tests for the affected package.
    - Routes/auth/middleware/storage changes: run integration/API tests; add tests if missing.
    - Contract changes: update swagger/spec, frontend types, and integration tests together.
    - Provide a short validation report (what ran, what didn’t, risks).
2.  **Sync**: Update frontend `types.ts` if API changes.
3.  **Style**: Mimic existing Go patterns (e.g., `if err != nil`).
4.  **Gardening**: Update this file and `docs/` after major refactors.
5.  **Deps**: No new Go modules without strong justification.
6.  **Contracts**: Treat handlers, middleware chains, and callback payloads as externally consumed interfaces, not local implementation details.
7.  **Enterprise Readiness**: Auth, audit, quota, and observability are required parts of productization, not optional polish.

_End of Context_
