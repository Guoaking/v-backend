package config

type PermissionMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

var PermissionList = []PermissionMeta{
	{ID: "org.read", Name: "读取组织信息", Category: "Organization", Description: "读取组织基本信息"},
	{ID: "org.update", Name: "更新组织设置", Category: "Organization", Description: "更新组织配置与设置"},
	{ID: "org.delete", Name: "删除组织", Category: "Organization", Description: "删除组织"},
	{ID: "team.read", Name: "查看成员列表", Category: "Team", Description: "查看组织成员"},
	{ID: "team.invite", Name: "邀请成员", Category: "Team", Description: "邀请新成员加入"},
	{ID: "team.write", Name: "修改/删除成员", Category: "Team", Description: "修改或移除组织成员"},
	{ID: "billing.read", Name: "查看账单", Category: "Billing", Description: "查看计费与账单"},
	{ID: "billing.write", Name: "修改支付方式/订阅", Category: "Billing", Description: "修改支付方式与订阅"},
	{ID: "keys.read", Name: "查看API Key", Category: "API Keys", Description: "查看密钥列表"},
	{ID: "keys.write", Name: "创建/撤销 API Key", Category: "API Keys", Description: "创建或撤销密钥"},
	{ID: "logs.read", Name: "查看审计日志", Category: "Logs", Description: "查看审计与请求日志"},
}
