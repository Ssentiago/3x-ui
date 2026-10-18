package controller

import (
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

type PolicyController struct {
	policyService  service.PolicyService
	clientService  service.ClientService
	inboundService service.InboundService
	xrayService    service.XrayService
}

func NewPolicyController(g *gin.RouterGroup) *PolicyController {
	a := &PolicyController{}
	a.initRouter(g)
	return a
}

func (a *PolicyController) initRouter(g *gin.RouterGroup) {
	g.GET("/policies", a.list)
	g.GET("/policies/:id", a.get)
	g.POST("/policies/create", a.create)
	g.POST("/policies/:id/update", a.update)
	g.POST("/policies/:id/delete", a.delete)
	g.POST("/policies/preview", a.preview)
	g.POST("/policies/apply", a.apply)
}

func (a *PolicyController) list(c *gin.Context) {
	db := database.GetDB()
	rows, err := a.policyService.List(db)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *PolicyController) get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	row, err := a.policyService.Get(db, id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, row, nil)
}

func (a *PolicyController) create(c *gin.Context) {
	var body service.PolicyInput
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	row, err := a.policyService.Create(db, body)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, row, nil)
}

func (a *PolicyController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var body service.PolicyInput
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	if err := a.policyService.Update(db, id, body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"id": id}, nil)
}

func (a *PolicyController) delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	if err := a.policyService.Delete(db, id); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"id": id}, nil)
}

type policyPreviewRequest struct {
	Group    string `json:"group"`
	PolicyId int    `json:"policyId"`
}

func (a *PolicyController) preview(c *gin.Context) {
	var req policyPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	result, err := service.Preview(db, req.Group, req.PolicyId)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
}

func (a *PolicyController) apply(c *gin.Context) {
	var req policyPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	result, needRestart, err := service.Apply(db, &a.clientService, &a.inboundService, req.Group, req.PolicyId)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	jsonObj(c, result, nil)
	notifyClientsChanged()
}
