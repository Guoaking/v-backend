package api

import (
    "time"

    "kyc-service/internal/models"
    "kyc-service/internal/service"
    "kyc-service/pkg/logger"
    "kyc-service/pkg/utils"

    "github.com/gin-gonic/gin"
)

type UserInvitationHandler struct{ service *service.KYCService }

func NewUserInvitationHandler(svc *service.KYCService) *UserInvitationHandler { return &UserInvitationHandler{service: svc} }

type MyInvitationItem struct {
    ID               string    `json:"id"`
    OrganizationID   string    `json:"organization_id"`
    OrganizationName string    `json:"organization_name"`
    Role             string    `json:"role"`
    InviterName      string    `json:"inviter_name"`
    SentAt           time.Time `json:"sent_at"`
    ExpiresAt        time.Time `json:"expires_at"`
}

// GET /api/v1/users/me/invitations
func (h *UserInvitationHandler) ListMyInvitations(c *gin.Context) {
    email := c.GetString("userEmail")
    if email == "" {
        JSONError(c, CodeUnauthorized, "未授权")
        return
    }
    type row struct {
        ID         string
        OrgID      string
        OrgName    string
        Role       string
        Inviter    string
        CreatedAt  time.Time
        ExpiresAt  time.Time
    }
    var rows []row
    err := h.service.DB.Raw(`
        SELECT i.id, i.org_id AS org_id, o.name AS org_name, i.role, COALESCE(u.full_name, u.name, '') AS inviter,
               i.created_at, i.expires_at
        FROM invitations i
        LEFT JOIN organizations o ON o.id = i.org_id
        LEFT JOIN users u ON u.id = i.inviter_id
        WHERE i.email = ? AND i.status = 'pending'
        ORDER BY i.created_at DESC
    `, email).Scan(&rows).Error
    if err != nil {
        logger.GetLogger().WithError(err).Error("查询邀请失败")
        JSONError(c, CodeDatabaseError, "查询失败")
        return
    }
    items := make([]MyInvitationItem, len(rows))
    for i, r := range rows {
        items[i] = MyInvitationItem{ID: r.ID, OrganizationID: r.OrgID, OrganizationName: r.OrgName, Role: r.Role, InviterName: r.Inviter, SentAt: r.CreatedAt, ExpiresAt: r.ExpiresAt}
    }
    JSONSuccess(c, items)
}

// POST /api/v1/users/me/invitations/:id/accept
func (h *UserInvitationHandler) AcceptMyInvitation(c *gin.Context) {
    id := c.Param("id")
    if id == "" { JSONError(c, CodeMissingParameter, "缺少邀请ID") ; return }
    email := c.GetString("userEmail")
    userID := c.GetString("userID")
    var inv models.Invitation
    if err := h.service.DB.Where("id = ? AND email = ? AND status = 'pending'", id, email).First(&inv).Error; err != nil {
        JSONError(c, CodeNotFound, "邀请不存在或已处理")
        return
    }
    // 若已是成员，直接标记接受并返回成功
    var existing models.OrganizationMember
    _ = h.service.DB.Where("organization_id = ? AND user_id = ?", inv.OrgID, userID).First(&existing).Error
    if existing.ID == "" {
        m := &models.OrganizationMember{ID: utils.GenerateID(), OrganizationID: inv.OrgID, UserID: userID, Role: inv.Role, Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}
        if err := h.service.DB.Create(m).Error; err != nil {
            JSONError(c, CodeDatabaseError, "加入组织失败")
            return
        }
    }
    now := time.Now()
    if err := h.service.DB.Model(&models.Invitation{}).Where("id = ?", inv.ID).Updates(map[string]interface{}{"status": "accepted", "accepted_at": &now}).Error; err != nil {
        JSONError(c, CodeDatabaseError, "更新邀请失败")
        return
    }
    JSONSuccess(c, gin.H{"code": 0})
}

// POST /api/v1/users/me/invitations/:id/decline
func (h *UserInvitationHandler) DeclineMyInvitation(c *gin.Context) {
    id := c.Param("id")
    if id == "" { JSONError(c, CodeMissingParameter, "缺少邀请ID") ; return }
    email := c.GetString("userEmail")
    var inv models.Invitation
    if err := h.service.DB.Where("id = ? AND email = ? AND status = 'pending'", id, email).First(&inv).Error; err != nil {
        JSONError(c, CodeNotFound, "邀请不存在或已处理")
        return
    }
    if err := h.service.DB.Model(&models.Invitation{}).Where("id = ?", inv.ID).Update("status", "declined").Error; err != nil {
        JSONError(c, CodeDatabaseError, "更新失败")
        return
    }
    JSONSuccess(c, gin.H{"code": 0})
}
