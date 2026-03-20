package models

// -----------------------------------------------------------------------------
// System Permissions Constants
// -----------------------------------------------------------------------------

const (
	// Organization & Team
	PermOrgRead    = "org.read"
	PermOrgUpdate  = "org.update"
	PermOrgDelete  = "org.delete"
	PermTeamRead   = "team.read"
	PermTeamInvite = "team.invite"
	PermTeamWrite  = "team.write"

	// Billing
	PermBillingRead    = "billing.read"
	PermOrgBillingRead = "org.billing.read"
	PermBillingWrite   = "billing.write"

	// OAuth Apps
	PermOAuthRead  = "oauth.read"
	PermOAuthWrite = "oauth.write"

	// Logs & Audit
	PermLogsRead     = "logs.read"
	PermOrgUsageRead = "org.usage.read"
	PermOrgAudit     = "org.audit"

	// Notifications
	PermNotificationsSend = "notifications.send"
)