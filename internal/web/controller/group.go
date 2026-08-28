package controller

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

type GroupController struct {
	clientService       service.ClientService
	xrayService         service.XrayService
	tariffService       service.TariffService
	clientTariffService service.ClientTariffService
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
	g.POST("/groups/resetTariff", a.resetGroupTariff)
	g.POST("/groups/resetTraffic", a.resetTraffic)
	g.POST("/groups/bulkAdd", a.bulkAdd)
	g.POST("/groups/bulkRemove", a.bulkRemove)

	g.GET("/tariffs", a.listTariffs)
	g.GET("/tariffs/:id", a.getTariff)
	g.POST("/tariffs/create", a.createTariff)
	g.POST("/tariffs/:id/update", a.updateTariff)
	g.POST("/tariffs/:id/delete", a.deleteTariff)
	g.POST("/tariffs/:id/profiles", a.setTariffProfiles)
	g.POST("/tariffs/preview", a.previewTariffChain)
	g.POST("/overrideField", a.overrideField)
	g.POST("/returnToTariff", a.returnToTariff)

	NewProfileController(g)
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
	Name     string `json:"name"`
	TariffID *int   `json:"tariffId"`
}

func (a *GroupController) create(c *gin.Context) {
	var body groupCreateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.clientService.CreateGroup(body.Name, body.TariffID); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"name": body.Name}, nil)
	notifyClientsChanged()
}

type groupRenameBody struct {
	OldName  string `json:"oldName"`
	NewName  string `json:"newName"`
	TariffID *int   `json:"tariffId"`
}

func (a *GroupController) rename(c *gin.Context) {
	var body groupRenameBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	if body.OldName != body.NewName {
		if err := a.clientService.RenameGroup(body.OldName, body.NewName); err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
	}
	if body.TariffID != nil {
		var exists int64
		if err := db.Model(&model.ClientGroup{}).Where("name = ?", body.NewName).Count(&exists).Error; err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
		if exists == 0 {
			if err := db.Create(&model.ClientGroup{Name: body.NewName}).Error; err != nil {
				jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
				return
			}
		}
	}
	if err := db.Model(&model.ClientGroup{}).Where("name = ?", body.NewName).Update("tariff_id", body.TariffID).Error; err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if body.TariffID != nil {
		// Create active tariff entries for clients in the renamed group that
		// don't already have one.
		var ids []int
		db.Model(&model.ClientRecord{}).
			Where("group_name = ?", body.NewName).
			Pluck("id", &ids)
		if len(ids) > 0 {
			now := time.Now().UnixMilli()
			var existingActive []int
			db.Model(&model.ClientTariff{}).
				Where("client_id IN ? AND ended_at IS NULL", ids).
				Pluck("client_id", &existingActive)
			existingSet := make(map[int]struct{}, len(existingActive))
			for _, id := range existingActive {
				existingSet[id] = struct{}{}
			}
			var newCTs []model.ClientTariff
			for _, id := range ids {
				if _, ok := existingSet[id]; !ok {
					newCTs = append(newCTs, model.ClientTariff{
						ClientID: id, TariffID: *body.TariffID, StartedAt: now,
					})
				}
			}
			if len(newCTs) > 0 {
				db.Create(&newCTs)
			}
		}
		a.tariffService.RefreshTrafficForGroup(body.NewName)
	} else {
		a.tariffService.RefreshTrafficForGroupReset(body.NewName)
	}
	a.xrayService.SetToNeedRestart()
	jsonObj(c, gin.H{}, nil)
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
	if err := a.clientService.DeleteGroup(body.Name); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	a.tariffService.RefreshTrafficForGroupReset(body.Name)
	a.xrayService.SetToNeedRestart()
	jsonObj(c, gin.H{}, nil)
	notifyClientsChanged()
}

type resetGroupTariffBody struct {
	Name string `json:"name"`
}

func (a *GroupController) resetGroupTariff(c *gin.Context) {
	var body resetGroupTariffBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	if err := db.Model(&model.ClientGroup{}).Where("name = ?", body.Name).Update("tariff_id", nil).Error; err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var ids []int
	db.Model(&model.ClientRecord{}).Where("group_name = ?", body.Name).Pluck("id", &ids)
	if len(ids) > 0 {
		now := time.Now().UnixMilli()
		db.Model(&model.ClientTariff{}).
			Where("client_id IN ? AND ended_at IS NULL", ids).
			Update("ended_at", now)
	}
	a.tariffService.RefreshTrafficForGroupReset(body.Name)
	jsonMsg(c, I18nWeb(c, "saveSuccess"), nil)
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
	if err := a.clientService.AddToGroup(req.Emails, req.Group); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	db := database.GetDB()
	var grp model.ClientGroup
	if err := db.Where("name = ?", req.Group).First(&grp).Error; err == nil && grp.TariffID != nil {
		a.tariffService.RefreshTrafficForGroup(req.Group)
	}
	jsonObj(c, gin.H{}, nil)
	a.xrayService.SetToNeedRestart()
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
	if err := a.clientService.RemoveFromGroup(req.Emails); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{}, nil)
	a.xrayService.SetToNeedRestart()
	notifyClientsChanged()
}

func (a *GroupController) listTariffs(c *gin.Context) {
	tariffs, err := a.tariffService.List()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, tariffs, nil)
}

func (a *GroupController) getTariff(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	tariff, err := a.tariffService.Get(id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, tariff, nil)
}

type tariffBody struct {
	Name            string `json:"name"`
	TrafficStrategy string `json:"trafficStrategy"`
	InboundStrategy string `json:"inboundStrategy"`
}

func (a *GroupController) createTariff(c *gin.Context) {
	var body tariffBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	tariff, err := a.tariffService.Create(body.Name, service.TariffStrategies{
		TrafficStrategy: body.TrafficStrategy,
		InboundStrategy: body.InboundStrategy,
	})
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, tariff, nil)
}

func (a *GroupController) updateTariff(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var body tariffBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	tariff, err := a.tariffService.Update(id, body.Name, service.TariffStrategies{
		TrafficStrategy: body.TrafficStrategy,
		InboundStrategy: body.InboundStrategy,
	})
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, tariff, nil)
	notifyClientsChanged()
}

type setTariffProfilesBody struct {
	ProfileIds []service.ProfilePosition `json:"profileIds"`
}

func (a *GroupController) setTariffProfiles(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var body setTariffProfilesBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.tariffService.SetProfiles(id, body.ProfileIds); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "saveSuccess"), nil)
	a.xrayService.SetToNeedRestart()
	notifyClientsChanged()
}

func (a *GroupController) deleteTariff(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.tariffService.Delete(id); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "deleteSuccess"), nil)
}

type previewTariffChainBody struct {
	Profiles        []service.ChainProfilePreview `json:"profiles"`
	TrafficStrategy string                        `json:"trafficStrategy"`
	InboundStrategy string                        `json:"inboundStrategy"`
}

func (a *GroupController) previewTariffChain(c *gin.Context) {
	var body previewTariffChainBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	resolved := service.ResolveChainPreview(body.Profiles, body.TrafficStrategy, body.InboundStrategy)
	jsonObj(c, resolved, nil)
}

type overrideFieldBody struct {
	Email string `json:"email"`
	Field string `json:"field"`
}

func (a *GroupController) overrideField(c *gin.Context) {
	var body overrideFieldBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	logger.Infof("🔴 overrideField: email=%q field=%q", body.Email, body.Field)
	if err := a.clientTariffService.OverrideField(body.Email, body.Field); err != nil {
		if errors.Is(err, service.ErrNoActiveTariff) {
			jsonMsg(c, I18nWeb(c, "pages.clients.noActiveTariff"), err)
			return
		}
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "saveSuccess"), nil)
	notifyClientsChanged()
}

type returnToTariffBody struct {
	Email string `json:"email"`
	Field string `json:"field"`
}

func (a *GroupController) returnToTariff(c *gin.Context) {
	var body returnToTariffBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	logger.Infof("🔴 returnToTariff: email=%q field=%q", body.Email, body.Field)
	if err := a.clientTariffService.ReturnToTariff(body.Email, body.Field); err != nil {
		if errors.Is(err, service.ErrNoActiveTariff) {
			jsonMsg(c, I18nWeb(c, "pages.clients.noActiveTariff"), err)
			return
		}
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "saveSuccess"), nil)
	notifyClientsChanged()
}
