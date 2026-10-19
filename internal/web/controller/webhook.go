package controller

import (
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/web/middleware"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/webhook"

	"github.com/gin-gonic/gin"
)

// webhookEndpointForm is the create/update/test payload. Secret is write-only:
// blank means "unchanged" on update unless ClearSecret is set.
type webhookEndpointForm struct {
	Id               int    `json:"id" form:"id"`
	Name             string `json:"name" form:"name"`
	Url              string `json:"url" form:"url"`
	Secret           string `json:"secret" form:"secret"`
	ClearSecret      bool   `json:"clearSecret" form:"clearSecret"`
	Enabled          bool   `json:"enabled" form:"enabled"`
	EnabledEvents    string `json:"enabledEvents" form:"enabledEvents"`
	AllowPrivate     bool   `json:"allowPrivate" form:"allowPrivate"`
	BackupTransport  string `json:"backupTransport" form:"backupTransport"`
	BackupAttachFile bool   `json:"backupAttachFile" form:"backupAttachFile"`
}

// WebhookController manages outbound webhook notification endpoints.
type WebhookController struct {
	endpointService webhook.EndpointService
	settingService  service.SettingService
}

func NewWebhookController(g *gin.RouterGroup) *WebhookController {
	a := &WebhookController{}
	a.initRouter(g)
	return a
}

func (a *WebhookController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/webhooks")
	g.GET("", a.list)
	g.POST("/create", a.create)
	g.POST("/update/:id", a.update)
	g.POST("/delete/:id", a.delete)
	g.POST("/test", a.test)
}

func (a *WebhookController) list(c *gin.Context) {
	list, err := a.endpointService.List()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	jsonObj(c, list, nil)
}

func (a *WebhookController) create(c *gin.Context) {
	form, ok := middleware.BindAndValidate[webhookEndpointForm](c)
	if !ok {
		return
	}
	view, err := a.endpointService.Create(toEndpointView(form), form.Secret)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	jsonObj(c, view, nil)
}

func (a *WebhookController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	form, ok := middleware.BindAndValidate[webhookEndpointForm](c)
	if !ok {
		return
	}
	view, err := a.endpointService.Update(id, toEndpointView(form), form.Secret, form.ClearSecret)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	jsonObj(c, view, nil)
}

func (a *WebhookController) delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	if err := a.endpointService.Delete(id); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), nil)
}

// test pings either a stored endpoint (id > 0) or a draft definition.
func (a *WebhookController) test(c *gin.Context) {
	form, ok := middleware.BindAndValidate[webhookEndpointForm](c)
	if !ok {
		return
	}
	source := webhook.SourceName(&a.settingService)
	var result webhook.TestResult
	if form.Id > 0 {
		result = a.endpointService.Test(form.Id, source)
	} else {
		result = a.endpointService.TestEndpoint(toEndpointView(form), form.Secret, source)
	}
	c.JSON(200, gin.H{
		"success": result.Success,
		"stage":   result.Stage,
		"msg":     result.Message,
	})
}

func toEndpointView(form *webhookEndpointForm) webhook.EndpointView {
	return webhook.EndpointView{
		Id:               form.Id,
		Name:             form.Name,
		Url:              form.Url,
		Enabled:          form.Enabled,
		EnabledEvents:    form.EnabledEvents,
		AllowPrivate:     form.AllowPrivate,
		BackupTransport:  form.BackupTransport,
		BackupAttachFile: form.BackupAttachFile,
	}
}
