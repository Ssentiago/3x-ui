package model

// WebhookEndpoint is one outbound HTTP notification target. Secret never
// leaves the server: list/test views report HasSecret instead of the value.
type WebhookEndpoint struct {
	Id               int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Name             string `json:"name" form:"name" validate:"required,max=64"`
	Url              string `json:"url" form:"url" validate:"required,max=1024"`
	Secret           string `json:"-" gorm:"column:secret"`
	Enabled          bool   `json:"enabled" form:"enabled"`
	EnabledEvents    string `json:"enabledEvents" form:"enabledEvents" gorm:"type:text"`
	AllowPrivate     bool   `json:"allowPrivate" form:"allowPrivate"`
	BackupTransport  string `json:"backupTransport" form:"backupTransport" gorm:"type:text;default:raw"`
	BackupAttachFile bool   `json:"backupAttachFile" form:"backupAttachFile"`
	HasSecret        bool   `json:"hasSecret" gorm:"-"`
	CreatedAt        int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt        int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}
