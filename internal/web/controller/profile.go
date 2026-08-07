package controller

import (
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

type ProfileController struct {
	profileService service.ProfileService
}

func NewProfileController(g *gin.RouterGroup) *ProfileController {
	a := &ProfileController{}
	a.initRouter(g)
	return a
}

func (a *ProfileController) initRouter(g *gin.RouterGroup) {
	g.GET("/profiles", a.list)
	g.GET("/profiles/:id", a.get)
	g.POST("/profiles/create", a.create)
	g.POST("/profiles/:id/update", a.update)
	g.POST("/profiles/:id/delete", a.delete)
}

func (a *ProfileController) list(c *gin.Context) {
	profiles, err := a.profileService.List()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, profiles, nil)
}

func (a *ProfileController) get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	profile, err := a.profileService.Get(id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, profile, nil)
}

type profileBody struct {
	Name       string `json:"name"`
	Traffic    *int64 `json:"traffic"`
	ExpiryDays *int   `json:"expiryDays"`
	LimitIP    *int   `json:"limitIp"`
	InboundIds []int  `json:"inboundIds"`
}

func (a *ProfileController) create(c *gin.Context) {
	var body profileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	profile, err := a.profileService.Create(body.Name, body.Traffic, body.ExpiryDays, body.LimitIP, body.InboundIds)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, profile, nil)
}

func (a *ProfileController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var body profileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	profile, err := a.profileService.Update(id, body.Name, body.Traffic, body.ExpiryDays, body.LimitIP, body.InboundIds)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, profile, nil)
}

func (a *ProfileController) delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.profileService.Delete(id); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "deleteSuccess"), nil)
}
