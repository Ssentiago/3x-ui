package controller

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

type GroupController struct {
	clientService service.ClientService
	xrayService   service.XrayService
}

func NewGroupController(g *gin.RouterGroup) *GroupController {
	a := &GroupController{}
	a.initRouter(g)
	return a
}

func (a *GroupController) initRouter(g *gin.RouterGroup) {
	g.GET("/groups", a.list)
	g.GET("/groups/:name/emails", a.emails)
	g.POST("/groups/create", a.create)
	g.POST("/groups/rename", a.rename)
	g.POST("/groups/delete", a.delete)
	g.POST("/groups/resetTraffic", a.resetTraffic)
	g.POST("/groups/bulkAdd", a.bulkAdd)
	g.POST("/groups/bulkRemove", a.bulkRemove)
	g.GET("/groups/:name/exceptions", a.listExceptions)
	g.POST("/groups/:name/exceptions/set", a.setExceptions)
	g.POST("/groups/:name/exceptions/add", a.addException)
	g.POST("/groups/:name/exceptions/remove", a.removeException)
}

func (a *GroupController) list(c *gin.Context) {
	rows, err := a.clientService.ListGroups()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *GroupController) emails(c *gin.Context) {
	name := c.Param("name")
	emails, err := a.clientService.EmailsByGroup(name)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, emails, nil)
}

type groupCreateBody struct {
	Name string `json:"name"`
}

func (a *GroupController) create(c *gin.Context) {
	var body groupCreateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.clientService.CreateGroup(body.Name); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"name": body.Name}, nil)
	notifyClientsChanged()
}

type groupRenameBody struct {
	OldName string `json:"oldName"`
	NewName string `json:"newName"`
}

func (a *GroupController) rename(c *gin.Context) {
	var body groupRenameBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	affected, err := a.clientService.RenameGroup(body.OldName, body.NewName)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	a.xrayService.SetToNeedRestart()
	jsonObj(c, gin.H{"affected": affected}, nil)
	notifyClientsChanged()
}

type groupDeleteBody struct {
	Name string `json:"name"`
}

func (a *GroupController) delete(c *gin.Context) {
	var body groupDeleteBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	affected, err := a.clientService.DeleteGroup(body.Name)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	a.xrayService.SetToNeedRestart()
	jsonObj(c, gin.H{"affected": affected}, nil)
	notifyClientsChanged()
}

type groupResetTrafficBody struct {
	Name string `json:"name"`
}

func (a *GroupController) resetTraffic(c *gin.Context) {
	var body groupResetTrafficBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.clientService.ResetGroupTraffic(body.Name); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"name": body.Name}, nil)
	notifyClientsChanged()
}

type bulkAddToGroupRequest struct {
	Emails []string `json:"emails"`
	Group  string   `json:"group"`
}

func (a *GroupController) bulkAdd(c *gin.Context) {
	var req bulkAddToGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if strings.TrimSpace(req.Group) == "" {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), common.NewError("group name is required"))
		return
	}
	affected, err := a.clientService.AddToGroup(req.Emails, req.Group)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"affected": affected}, nil)
	notifyClientsChanged()
}

type bulkRemoveFromGroupRequest struct {
	Emails []string `json:"emails"`
}

func (a *GroupController) bulkRemove(c *gin.Context) {
	var req bulkRemoveFromGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	affected, err := a.clientService.RemoveFromGroup(req.Emails)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"affected": affected}, nil)
	a.xrayService.SetToNeedRestart()
	notifyClientsChanged()
}

func (a *GroupController) listExceptions(c *gin.Context) {
	name := c.Param("name")
	db := database.GetDB()
	emails, err := service.ListExceptions(db, name)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, emails, nil)
}

type setExceptionsRequest struct {
	Emails []string `json:"emails"`
}

func (a *GroupController) setExceptions(c *gin.Context) {
	name := c.Param("name")
	var req setExceptionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	if err := service.SetExceptions(db, name, req.Emails); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"group": name, "count": len(req.Emails)}, nil)
}

type exceptionEmailRequest struct {
	Email string `json:"email"`
}

func (a *GroupController) addException(c *gin.Context) {
	name := c.Param("name")
	var req exceptionEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	if err := service.AddException(db, name, req.Email); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"group": name, "email": req.Email}, nil)
}

func (a *GroupController) removeException(c *gin.Context) {
	name := c.Param("name")
	var req exceptionEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	if err := service.RemoveException(db, name, req.Email); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"group": name, "email": req.Email}, nil)
}
