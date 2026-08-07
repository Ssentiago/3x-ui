package service

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

type TariffService struct{}

type TariffSummary struct {
	Id              int                       `json:"id"`
	Name            string                    `json:"name"`
	TrafficStrategy string                    `json:"trafficStrategy"`
	InboundStrategy string                    `json:"inboundStrategy"`
	Enable          bool                      `json:"enable"`
	Profiles        []model.TariffProfileItem `json:"profiles,omitempty" gorm:"-"`
	Resolved        *model.ResolvedFields     `json:"resolved,omitempty" gorm:"-"`
	GroupCount      int                       `json:"groupCount"`
	ClientCount     int                       `json:"clientCount"`
	CreatedAt       int64                     `json:"createdAt"`
	UpdatedAt       int64                     `json:"updatedAt"`
}

type TariffStrategies struct {
	TrafficStrategy string
	InboundStrategy string
}

type ProfilePosition struct {
	Id       int `json:"id"`
	Position int `json:"position"`
}

func (s *TariffService) List() ([]TariffSummary, error) {
	db := database.GetDB()
	var tariffs []model.Tariff
	if err := db.Order("id asc").Find(&tariffs).Error; err != nil {
		return nil, err
	}

	groupCounts := s.batchCountGroupsByTariffId(len(tariffs))

	var summaries []TariffSummary
	for _, t := range tariffs {
		summaries = append(summaries, TariffSummary{
			Id:              t.Id,
			Name:            t.Name,
			TrafficStrategy: t.TrafficStrategy,
			InboundStrategy: t.InboundStrategy,
			Enable:          t.Enable,
			GroupCount:      groupCounts[t.Id],
			CreatedAt:       t.CreatedAt,
			UpdatedAt:       t.UpdatedAt,
		})
	}
	return summaries, nil
}

func (s *TariffService) Get(id int) (*TariffSummary, error) {
	var t model.Tariff
	if err := database.GetDB().First(&t, id).Error; err != nil {
		return nil, err
	}
	groupCount, _ := s.countGroupsByTariffId(t.Id)
	clientCount, _ := s.countClientsByTariffId(t.Id)
	profileItems := s.listProfileItems(t.Id)
	resolved := s.resolveTariff(&t)

	return &TariffSummary{
		Id:              t.Id,
		Name:            t.Name,
		TrafficStrategy: t.TrafficStrategy,
		InboundStrategy: t.InboundStrategy,
		Enable:          t.Enable,
		Profiles:        profileItems,
		Resolved:        resolved,
		GroupCount:      groupCount,
		ClientCount:     clientCount,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}, nil
}

func defaultStrategies(strategies *TariffStrategies) {
	if strategies.TrafficStrategy == "" {
		strategies.TrafficStrategy = model.StrategyOverwrite
	}
	if strategies.InboundStrategy == "" {
		strategies.InboundStrategy = model.StrategyOverwrite
	}
}

func (s *TariffService) Create(name string, strategies TariffStrategies) (*TariffSummary, error) {
	if name == "" {
		return nil, errors.New("tariff name is required")
	}
	defaultStrategies(&strategies)
	t := model.Tariff{
		Name:            name,
		TrafficStrategy: strategies.TrafficStrategy,
		InboundStrategy: strategies.InboundStrategy,
	}
	if err := database.GetDB().Create(&t).Error; err != nil {
		return nil, err
	}
	return s.Get(t.Id)
}

func (s *TariffService) Update(id int, name string, strategies TariffStrategies) (*TariffSummary, error) {
	var t model.Tariff
	if err := database.GetDB().First(&t, id).Error; err != nil {
		return nil, err
	}
	if name == "" {
		return nil, errors.New("tariff name is required")
	}
	defaultStrategies(&strategies)
	t.Name = name
	t.TrafficStrategy = strategies.TrafficStrategy
	t.InboundStrategy = strategies.InboundStrategy
	if err := database.GetDB().Save(&t).Error; err != nil {
		return nil, err
	}
	s.refreshTariffTraffic(&t)
	return s.Get(t.Id)
}

func (s *TariffService) SetProfiles(id int, profileIds []ProfilePosition) error {
	for i, pp := range profileIds {
		if pp.Position != i {
			return fmt.Errorf("profile positions must be contiguous starting from 0, got position %d at index %d", pp.Position, i)
		}
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		tx.Where("tariff_id = ?", id).Delete(&model.TariffProfile{})
		for _, pp := range profileIds {
			tx.Create(&model.TariffProfile{
				TariffID:  id,
				ProfileID: pp.Id,
				Position:  pp.Position,
			})
		}
		return nil
	})
}

func (s *TariffService) Delete(id int) error {
	groupCount, err := s.countGroupsByTariffId(id)
	if err != nil {
		return err
	}
	if groupCount > 0 {
		return fmt.Errorf("tariff is used by %d group(s), unlink before deleting", groupCount)
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		tx.Where("tariff_id = ?", id).Delete(&model.TariffProfile{})
		return tx.Delete(&model.Tariff{}, id).Error
	})
}

func rewriteTrafficForClients(db *gorm.DB, clients []model.ClientRecord, resolved bool) {
	for i := range clients {
		var total, expiry int64
		if resolved {
			f := ResolveClientLimits(db, &clients[i])
			total = f.TotalGB
			expiry = f.ExpiryTime
		} else {
			total = clients[i].TotalGB
			expiry = clients[i].ExpiryTime
		}
		updates := map[string]any{"total": total, "expiry_time": expiry}
		db.Table("client_traffics").Where("email = ?", clients[i].Email).Updates(updates)
	}
}

// RefreshTrafficForGroupReset rewrites client_traffics.total and expiry_time for
// every client in a group from the client row's default values (ignoring
// tariff-effective values). Called on group traffic reset and tariff unbind.
// client_traffics is a statistics cache — the source of truth is always the
// effective client record resolved at read time. Override-controlled fields are
// left untouched.
func (s *TariffService) RefreshTrafficForGroupReset(groupName string) {
	db := database.GetDB()
	var clients []model.ClientRecord
	if err := db.Where("group_name = ?", groupName).Find(&clients).Error; err != nil {
		return
	}
	rewriteTrafficForClients(db, clients, false)
}

// RefreshTrafficForGroup rewrites client_traffics.total and expiry_time with
// tariff-effective values for every client in a tariff-bound group. Called after
// tariff/profile edits to keep the statistics cache consistent with the resolved
// chain. Override-controlled fields are skipped. client_traffics is a cache —
// the authoritative values are resolved at read time via ResolveClientFields.
func (s *TariffService) RefreshTrafficForGroup(groupName string) {
	db := database.GetDB()
	var grp model.ClientGroup
	if err := db.Where("name = ?", groupName).First(&grp).Error; err != nil || grp.TariffID == nil {
		return
	}
	var clients []model.ClientRecord
	if err := db.Where("group_name = ?", groupName).Find(&clients).Error; err != nil {
		return
	}
	rewriteTrafficForClients(db, clients, true)
}

func (s *TariffService) refreshTariffTraffic(tariff *model.Tariff) {
	db := database.GetDB()
	var groups []model.ClientGroup
	if err := db.Where("tariff_id = ?", tariff.Id).Find(&groups).Error; err != nil {
		return
	}
	if len(groups) == 0 {
		return
	}
	groupNames := make([]string, 0, len(groups))
	for _, g := range groups {
		groupNames = append(groupNames, g.Name)
	}
	var clients []model.ClientRecord
	if err := db.Where("group_name IN ?", groupNames).Find(&clients).Error; err != nil {
		return
	}
	rewriteTrafficForClients(db, clients, true)
}

func (s *TariffService) listProfileItems(tariffId int) []model.TariffProfileItem {
	var rows []struct {
		Id       int
		Name     string
		Position int
	}
	database.GetDB().Table("profiles").
		Select("profiles.id, profiles.name, tariff_profiles.position").
		Joins("JOIN tariff_profiles ON tariff_profiles.profile_id = profiles.id").
		Where("tariff_profiles.tariff_id = ?", tariffId).
		Order("tariff_profiles.position ASC").
		Scan(&rows)
	items := make([]model.TariffProfileItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, model.TariffProfileItem{
			Id:       r.Id,
			Name:     r.Name,
			Position: r.Position,
		})
	}
	return items
}

func (s *TariffService) resolveTariff(t *model.Tariff) *model.ResolvedFields {
	db := database.GetDB()
	var profiles []model.Profile
	db.Table("profiles").
		Joins("JOIN tariff_profiles ON tariff_profiles.profile_id = profiles.id").
		Where("tariff_profiles.tariff_id = ?", t.Id).
		Order("tariff_profiles.position ASC").
		Find(&profiles)
	ctx := &tariffContext{Tariff: t, Profiles: profiles}
	chain := resolveChain(ctx)
	return &model.ResolvedFields{
		Traffic:    chain.Traffic,
		ExpiryDays: chain.ExpiryDays,
		LimitIP:    chain.LimitIP,
		InboundIds: chain.InboundIds,
	}
}

func (s *TariffService) countGroupsByTariffId(tariffId int) (int, error) {
	var count int64
	err := database.GetDB().Model(&model.ClientGroup{}).Where("tariff_id = ?", tariffId).Count(&count).Error
	return int(count), err
}

func (s *TariffService) countClientsByTariffId(tariffId int) (int, error) {
	var groups []model.ClientGroup
	if err := database.GetDB().Where("tariff_id = ?", tariffId).Find(&groups).Error; err != nil {
		return 0, err
	}
	if len(groups) == 0 {
		return 0, nil
	}
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	var count int64
	err := database.GetDB().Model(&model.ClientRecord{}).Where("group_name IN ?", names).Count(&count).Error
	return int(count), err
}

func (s *TariffService) batchCountGroupsByTariffId(tariffCount int) map[int]int {
	if tariffCount == 0 {
		return map[int]int{}
	}
	type row struct {
		TariffID int
		Count    int
	}
	var rows []row
	database.GetDB().Model(&model.ClientGroup{}).
		Select("tariff_id, COUNT(*) as count").
		Where("tariff_id IS NOT NULL").
		Group("tariff_id").
		Scan(&rows)
	counts := make(map[int]int, len(rows))
	for _, r := range rows {
		counts[r.TariffID] = r.Count
	}
	return counts
}

func parseInboundIds(raw string) ([]int, error) {
	if raw == "" || raw == "null" {
		return []int{}, nil
	}
	var ids []int
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func marshalInboundIds(ids []int) (string, error) {
	if ids == nil {
		return "", nil
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
