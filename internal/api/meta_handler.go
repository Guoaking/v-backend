package api

import (
	"kyc-service/internal/models"
	"kyc-service/internal/service"
	"time"

	"github.com/gin-gonic/gin"
)

type MetaHandler struct{ service *service.KYCService }

func NewMetaHandler(s *service.KYCService) *MetaHandler { return &MetaHandler{service: s} }

func (h *MetaHandler) GetPermissions(c *gin.Context) {
	var perms []models.Permission
	if err := h.service.DB.Order("id ASC").Find(&perms).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	JSONSuccess(c, perms)
}

type RoleWithPerms struct {
	models.Role
	Permissions []string `json:"permissions"`
}

func (h *MetaHandler) GetRoles(c *gin.Context) {
	var roles []models.Role
	if err := h.service.DB.Order("created_at ASC").Find(&roles).Error; err != nil {
		JSONError(c, CodeDatabaseError, "查询失败")
		return
	}
	resp := make([]RoleWithPerms, len(roles))
	for i, r := range roles {
		var rows []struct{ PermissionID string }
		var ids []string
		_ = h.service.DB.Table("role_permissions").Select("permission_id").Where("role_id = ?", r.ID).Scan(&rows).Error
		for _, rr := range rows {
			ids = append(ids, rr.PermissionID)
		}
		resp[i] = RoleWithPerms{Role: r, Permissions: ids}
	}
	JSONSuccess(c, resp)
}

type RoleUpdateRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

func (h *MetaHandler) CreateRole(c *gin.Context) {
	id := c.Param("id")
	var req RoleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	role := models.Role{ID: id, Name: req.Name, Description: req.Description, IsSystem: false, CreatedAt: time.Now()}
	tx := h.service.DB.Begin()
	if err := tx.Create(&role).Error; err != nil {
		tx.Rollback()
		JSONError(c, CodeDatabaseError, "创建失败")
		return
	}
	for _, pid := range req.Permissions {
		_ = tx.Create(&models.RolePermission{RoleID: id, PermissionID: pid}).Error
	}
	if err := tx.Commit().Error; err != nil {
		JSONError(c, CodeDatabaseError, "创建失败")
		return
	}
	JSONSuccess(c, role)
}

func (h *MetaHandler) UpdateRole(c *gin.Context) {
	id := c.Param("id")
	var role models.Role
	if err := h.service.DB.First(&role, "id = ?", id).Error; err != nil {
		JSONError(c, CodeNotFound, "角色不存在")
		return
	}
	var req RoleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSONError(c, CodeInvalidParameter, "参数验证失败")
		return
	}
	// 系统角色：允许新增权限，但禁止移除已有权限
	// 非系统角色：允许完整覆盖权限集合
	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if len(updates) > 0 {
		if err := h.service.DB.Model(&role).Updates(updates).Error; err != nil {
			JSONError(c, CodeDatabaseError, "更新失败")
			return
		}
	}
	if req.Permissions != nil {
		if role.IsSystem {
			var rows []struct{ PermissionID string }
			_ = h.service.DB.Table("role_permissions").Select("permission_id").Where("role_id = ?", id).Scan(&rows).Error
			cur := map[string]struct{}{}
			for _, r := range rows {
				cur[r.PermissionID] = struct{}{}
			}
			reqSet := map[string]struct{}{}
			for _, p := range req.Permissions {
				reqSet[p] = struct{}{}
			}
			// 检查是否尝试移除已有权限
			for p := range cur {
				if _, ok := reqSet[p]; !ok {
					JSONError(c, CodeForbidden, "系统角色不可移除权限")
					return
				}
			}
			// 仅新增缺失权限
			tx := h.service.DB.Begin()
			for _, p := range req.Permissions {
				if _, ok := cur[p]; !ok {
					if err := tx.Create(&models.RolePermission{RoleID: id, PermissionID: p}).Error; err != nil {
						tx.Rollback()
						JSONError(c, CodeDatabaseError, "更新失败")
						return
					}
				}
			}
			if err := tx.Commit().Error; err != nil {
				JSONError(c, CodeDatabaseError, "更新失败")
				return
			}
		} else {
			tx := h.service.DB.Begin()
			if err := tx.Where("role_id = ?", id).Delete(&models.RolePermission{}).Error; err != nil {
				tx.Rollback()
				JSONError(c, CodeDatabaseError, "更新失败")
				return
			}
			for _, pid := range req.Permissions {
				_ = tx.Create(&models.RolePermission{RoleID: id, PermissionID: pid}).Error
			}
			if err := tx.Commit().Error; err != nil {
				JSONError(c, CodeDatabaseError, "更新失败")
				return
			}
		}
	}
	JSONSuccess(c, role)
}

func (h *MetaHandler) DeleteRole(c *gin.Context) {
	id := c.Param("id")
	var role models.Role
	if err := h.service.DB.First(&role, "id = ?", id).Error; err != nil {
		JSONError(c, CodeNotFound, "角色不存在")
		return
	}
	if role.IsSystem {
		JSONError(c, CodeForbidden, "系统角色不可删除")
		return
	}
	var cnt int64
	if err := h.service.DB.Model(&models.OrganizationMember{}).Where("role = ?", id).Count(&cnt).Error; err == nil && cnt > 0 {
		JSONError(c, CodeConflict, "仍有成员关联该角色")
		return
	}
	if err := h.service.DB.Delete(&role).Error; err != nil {
		JSONError(c, CodeDatabaseError, "删除失败")
		return
	}
	JSONSuccess(c, gin.H{"deleted": id})
}
