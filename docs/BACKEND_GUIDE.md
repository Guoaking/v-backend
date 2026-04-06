# Backend Developer Guide

> **Agent Instruction**: This is the entry point for backend development standards.
> For architecture and specs, check the subdirectories here.

## 1. Documentation Map

| Category           | Document                                                                               | Description                               |
| ------------------ | -------------------------------------------------------------------------------------- | ----------------------------------------- |
| **Architecture**   | [architecture/AUTH_UNIFICATION.md](architecture/AUTH_UNIFICATION.md)                   | Auth unification plan (API Key + OAuth2)  |
|                    | [architecture/ATTENDANCE_APP_DESIGN.md](architecture/ATTENDANCE_APP_DESIGN.md)         | Attendance pilot background and design    |
|                    | [architecture/RBAC_DESIGN.md](architecture/RBAC_DESIGN.md)                             | Role-Based Access Control implementation  |
|                    | [architecture/OTEL_MONITORING.md](architecture/OTEL_MONITORING.md)                     | OpenTelemetry & Monitoring setup          |
| **Specs**          | [specs/action_liveness_backend_spec.md](specs/action_liveness_backend_spec.md)         | Action Liveness logic specification       |
|                    | [specs/attendance_app_spec.md](specs/attendance_app_spec.md)                           | Attendance BFF current contract           |
|                    | [specs/attendance_p00_blueprint.md](specs/attendance_p00_blueprint.md)                 | Attendance domain-boundary baseline       |
| **API**            | [swagger.json](swagger.json)                                                           | OpenAPI 3.0 definition                    |
| **Storage**        | `internal/storage/storage_service.go`                                                  | Policy-based Storage Resolver             |
| **Guides**         | [guides/CI_SETUP.md](guides/CI_SETUP.md)                                               | CI/CD pipeline setup                      |
| **Knowledge Base** | [kb/AI_KNOWLEDGE_BASE.md](kb/AI_KNOWLEDGE_BASE.md)                                     | Code snippets and how-tos                 |

## 2. Core Principles (Core Principles)

- **Architecture Consistency**:
  - **AuthN**: Follow [Unified Auth Plan](architecture/AUTH_UNIFICATION.md). Prefer OAuth2/JWT.
  - **Storage**: Use `StorageService.ResolveAccess` for all file retrieval. Never hardcode `/data` or `/mnt`.
  - **Billing**: All cost-incurring ops MUST call `checkAndConsumeQuota`.

- **API Contract First**:
  - Define Request/Response structs explicitly. No `map[string]any`.
  - **CRITICAL**: ALL API responses MUST use the standard wrapper functions from `pkg/response/response.go` (e.g., `response.JSONSuccess`, `response.JSONError`, `response.JSONErrorWithStatus`).
  - **PROHIBITED**: NEVER use raw `c.JSON()` or `c.AbortWithStatusJSON()` in any handlers or middleware. This breaks the unified response structure (`{code, message, timestamp, request_id, data/error}`) that frontend interceptors rely on.
  - **Storage Redirects**: Production environments use `X-Accel-Redirect` via Nginx. The backend should return the internal path in the header.

## 3. Feature Development Checklist

- [ ] **API Spec**: Defined structs and Swagger annotations?
- [ ] **Auth**: Protected by middleware?
- [ ] **Storage**: Using policy-based routing for uploads and access?
- [ ] **Quota**: `checkAndConsumeQuota` called?
- [ ] **Audit**: `KYCRequest` and `AuditLog` recorded?
- [ ] **Observability**: `tracing.StartSpan` and `metrics` added?

---

## 4. References

- [AI First Knowledge Base](kb/AI_KNOWLEDGE_BASE.md)
- [Auth Unification Plan](architecture/AUTH_UNIFICATION.md)
