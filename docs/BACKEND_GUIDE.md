# Backend Developer Guide

> **Agent Instruction**: This is the entry point for backend development standards.
> For architecture and specs, check the subdirectories here.

## 1. Documentation Map

| Category           | Document                                                                               | Description                               |
| ------------------ | -------------------------------------------------------------------------------------- | ----------------------------------------- |
| **Architecture**   | [architecture/AUTH_UNIFICATION.md](architecture/AUTH_UNIFICATION.md)                   | Auth unification plan (API Key + OAuth2)  |
|                    | [architecture/PLAYGROUND_AUTH_EVOLUTION.md](architecture/PLAYGROUND_AUTH_EVOLUTION.md) | Playground STS & Secret Visibility Design |
|                    | [architecture/RBAC_DESIGN.md](architecture/RBAC_DESIGN.md)                             | Role-Based Access Control implementation  |
|                    | [architecture/OTEL_MONITORING.md](architecture/OTEL_MONITORING.md)                     | OpenTelemetry & Monitoring setup          |
| **Specs**          | [specs/action_liveness_backend_spec.md](specs/action_liveness_backend_spec.md)         | Action Liveness logic specification       |
| **API**            | [api/swagger.json](api/swagger.json)                                                   | OpenAPI 3.0 definition                    |
| **Guides**         | [guides/CI_SETUP.md](guides/CI_SETUP.md)                                               | CI/CD pipeline setup                      |
| **Knowledge Base** | [kb/AI_KNOWLEDGE_BASE.md](kb/AI_KNOWLEDGE_BASE.md)                                     | Code snippets and how-tos                 |

## 2. Core Principles (Core Principles)

- **Architecture Consistency**:
  - **AuthN**: Follow [Unified Auth Plan](architecture/AUTH_UNIFICATION.md). Prefer OAuth2/JWT.
  - **Billing**: All cost-incurring ops MUST call `checkAndConsumeQuota`.

- **API Contract First**:
  - Define Request/Response structs explicitly. No `map[string]any`.
  - Use standard `Code*` error constants.

## 3. Feature Development Checklist

- [ ] **API Spec**: Defined structs and Swagger annotations?
- [ ] **Auth**: Protected by middleware?
- [ ] **Quota**: `checkAndConsumeQuota` called?
- [ ] **Audit**: `KYCRequest` and `AuditLog` recorded?
- [ ] **Observability**: `tracing.StartSpan` and `metrics` added?

---

## 4. References

- [AI First Knowledge Base](kb/AI_KNOWLEDGE_BASE.md)
- [Auth Unification Plan](architecture/AUTH_UNIFICATION.md)
