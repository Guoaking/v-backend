# Attendance P00 Blueprint

## 1. Purpose

This document defines the **P00 baseline** for Attendance.

P00 is not a feature phase. It is the architecture and domain-baseline phase that must be completed before expanding Attendance into a full web attendance and management system.

The goal is to turn Attendance from a pilot BFF into a **clear application domain** that:

- has explicit application boundaries
- does not leak business semantics into platform capabilities
- owns its own business data model
- owns its own state model
- owns its own admin semantics and reporting vocabulary
- integrates with platform capabilities through explicit contracts

## 2. What P00 Is / Is Not

### 2.1 P00 Is

- a domain-boundary definition
- a shared language definition
- a data ownership definition
- a state model definition
- an API ownership definition
- a phased migration baseline for P0 and P1

### 2.2 P00 Is Not

- a full HR system
- a payroll system
- a complete scheduling engine
- a complete approval workflow
- a database split or microservice split

## 3. Layer Ownership

### 3.1 Shared Foundations

Owned by the platform:

- Organization / Tenant
- Console Users / Roles / Permissions
- OAuth / STS / JWT
- Redis / Task / MQ
- Logging / Tracing / Metrics / Audit
- runtime configuration

Attendance consumes these capabilities and must not duplicate or fork them.

### 3.2 Platform Capabilities

Owned by the platform:

- OCR
- Face Detect
- Face Compare
- Liveness
- Storage
- Quota / Usage / billing-grade metering

These capabilities should stay generic and reusable. They must not absorb business concepts like late arrival, missing punch, store policy, or schedule semantics.

### 3.3 Attendance Application Domain

Owned by Attendance:

- employee business identity
- enrollment flow
- punch flow
- punch review
- group / site / shift / assignment
- status projection
- admin views
- report semantics

## 4. Domain Language

### 4.1 Canonical Identity

- `employee_id`
  - internal immutable primary key
  - system-owned
- `employee_no`
  - stable business identifier inside one organization
  - default business-facing employee key
- `id_number`
  - government / real-name identity number
  - used for registration, deduplication, and compliance anchor
  - not the primary business key for day-to-day attendance operations
- `employee_sn`
  - legacy optional field
  - enters deprecation status in P00
  - if future external mapping is needed, evolve to `external_employee_code`

### 4.2 Core Domain Objects

- `AttendanceEmployee`
- `AttendancePolicy`
- `PunchEvent`
- `PunchReview`
- `AttendanceGroup`
- `AttendanceGroupMembership`
- `AttendanceSite`
- `AttendanceShiftTemplate`
- `AttendanceShiftAssignment`
- `AttendanceStatusSnapshot`
- `ReportReadModel`

Current schema landing already includes:

- `attendance_groups`
- `attendance_group_memberships`
- `attendance_sites`
- `attendance_shift_templates`
- `attendance_shift_assignments`
- `attendance_punch_reviews`
- `attendance_status_snapshots`

## 5. Data Ownership

### 5.1 Shared Core Data

Remain owned by the platform:

- organizations
- console users
- roles / permissions
- oauth / sts credentials
- global audit log
- global usage log / billing records

### 5.2 Attendance-Owned Data

Owned by Attendance:

- employee master data
- attendance business configuration
- punch events
- punch review decisions
- group / site / shift / assignment data
- status snapshots
- report read models

### 5.3 Database Strategy

Current recommendation:

- same DB instance
- domain-separated tables or prefixes
- no casual duplication of shared core tables
- no ad-hoc cross-domain joins in business queries

If future scale, SLA, compliance, or release cadence requires it, Attendance may move toward DB-level isolation. Even then, shared foundations should still be integrated through IDs, controlled queries, events, or read projections rather than duplicated source-of-truth tables.

## 6. State Model Baseline

### 6.1 Employee Lifecycle

- `pending_enrollment`
- `active`
- `inactive`
- `deleted`

### 6.2 Enrollment Flow

- `ocr_collected`
- `identity_confirmed`
- `face_verified`
- `enrolled`
- failure reasons recorded separately

### 6.3 Punch Event State

- `submitted`
- `verified`
- `manual_review_required`
- `rejected`

### 6.4 Review State

- `pending`
- `approved`
- `rejected`

### 6.5 Daily Status Snapshot

- `not_scheduled`
- `scheduled`
- `checked_in`
- `checked_out`
- `late`
- `missing_punch`
- `review_pending`
- `exception`

`PunchEvent` records facts. `StatusSnapshot` answers management and reporting questions. They must not be treated as the same thing.

## 7. API Boundary Baseline

### 7.1 Employee-Facing Boundary

Employee H5 should only call:

- `/api/v1/attendance/enroll/*`
- `/api/v1/attendance/punch/*`

It should not call platform capability routes like `/api/v1/kyc/liveness/action/*` directly once P0 is complete.

### 7.2 Admin Boundary

Console should only call:

- `/api/v1/console/attendance/*`

Attendance admin endpoints must rely on explicit Console JWT, org context, and permission boundaries rather than ad-hoc query parameters as the primary security model.

### 7.3 Capability Integration Boundary

Attendance may orchestrate OCR / Face / Liveness / Storage internally, but the capability invocation contract should stay platform-owned and reusable. Business policies must remain inside Attendance.

## 8. P00 Exit Criteria

P00 is considered complete when:

- Attendance boundary is documented and accepted
- canonical identity rules are fixed
- `employee_sn` is deprecated in design
- Attendance-owned entities are defined
- event / review / snapshot separation is defined
- employee-side and admin-side API ownership is documented
- platform/shared/application responsibilities are documented
- P0 implementation backlog is derived from this blueprint

## 9. Derived P0 Backlog

- close active-liveness direct calls from frontend to platform capability routes (completed)
- make admin auth boundary explicit in Attendance routes (completed with shared Console JWT/org-context chain; dedicated attendance permissions still pending)
- continue migrating employee-side identity handling toward `employee_no`
- keep employee self-service disabled until employee session auth exists
- mark `employee_sn` deprecated in model and contract language (completed at documentation/model-comment level; future external-code migration still pending)

## 10. Derived P1 Build Order

### 10.1 First

- groups / stores
- sites / location rules
- shift templates
- employee-group-site-shift relationships

### 10.2 Second

- shift assignments
- real-time status snapshots
- late / missing / offsite / review-pending semantics

### 10.3 Third

- reports
- exports
- employee timeline
- report read models

## 11. Design Guardrails

Before implementing any Attendance requirement, ask:

- Is this a shared foundation concern, a platform capability concern, or an Attendance application concern?
- Does this requirement introduce new business language, admin semantics, or report semantics?
- Will this change leak business rules into platform capability code?
- Does this change need a domain-owned table, event, or snapshot instead of extending a generic shared table?

If the answer is unclear, stop and document the boundary decision before coding.
