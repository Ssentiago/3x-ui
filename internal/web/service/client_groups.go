package service

import (
	"sort"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/gorm"
)

type GroupSummary struct {
	Name        string         `json:"name"`
	ClientCount int            `json:"clientCount"`
	TrafficUsed int64          `json:"trafficUsed"`
	Up          int64          `json:"up"`
	Down        int64          `json:"down"`
	TariffID    *int           `json:"tariffId"`
	TariffName  string         `json:"tariffName"`
	Tariff      *TariffSummary `json:"tariff"`
}

func (s *ClientService) ListGroups() ([]GroupSummary, error) {
	db := database.GetDB()
	var derived []GroupSummary
	if err := db.Table("clients AS c").
		Select("c.group_name AS name, COUNT(*) AS client_count, COALESCE(SUM(ct.up + ct.down), 0) AS traffic_used, COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
		Joins("LEFT JOIN client_traffics ct ON ct.email = c.email").
		Where("c.group_name <> ''").
		Group("c.group_name").
		Scan(&derived).Error; err != nil {
		return nil, err
	}
	var stored []model.ClientGroup
	if err := db.Find(&stored).Error; err != nil {
		return nil, err
	}
	storedMap := make(map[string]model.ClientGroup, len(stored))
	for _, g := range stored {
		storedMap[g.Name] = g
	}
	type groupAgg struct {
		count int
		up    int64
		down  int64
	}
	baseUp := make(map[string]int64, len(stored))
	baseDown := make(map[string]int64, len(stored))
	merged := make(map[string]groupAgg, len(derived)+len(stored))
	for _, g := range stored {
		merged[g.Name] = groupAgg{}
		baseUp[g.Name] = g.ResetUp
		baseDown[g.Name] = g.ResetDown
	}
	for _, g := range derived {
		merged[g.Name] = groupAgg{count: g.ClientCount, up: g.Up, down: g.Down}
	}
	tariffNames := make(map[int]string)
	tariffSummaries := make(map[int]*TariffSummary)
	if len(stored) > 0 {
		var tariffIDs []int
		for _, g := range stored {
			if g.TariffID != nil {
				tariffIDs = append(tariffIDs, *g.TariffID)
			}
		}
		if len(tariffIDs) > 0 {
			var tariffs []model.Tariff
			if err := db.Where("id IN ?", tariffIDs).Find(&tariffs).Error; err == nil {
				for _, t := range tariffs {
					tariffNames[t.Id] = t.Name
					tariffSummaries[t.Id] = &TariffSummary{
						Id:              t.Id,
						Name:            t.Name,
						TrafficStrategy: t.TrafficStrategy,
						InboundStrategy: t.InboundStrategy,
						Enable:          t.Enable,
					}
				}
			}
		}
	}
	out := make([]GroupSummary, 0, len(merged))
	for name, agg := range merged {
		up := max(agg.up-baseUp[name], 0)
		down := max(agg.down-baseDown[name], 0)
		gs := GroupSummary{Name: name, ClientCount: agg.count, TrafficUsed: up + down, Up: up, Down: down}
		if sg, ok := storedMap[name]; ok && sg.TariffID != nil {
			gs.TariffID = sg.TariffID
			gs.TariffName = tariffNames[*sg.TariffID]
			gs.Tariff = tariffSummaries[*sg.TariffID]
		}
		out = append(out, gs)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func adjustGroupBaselinesForRemovedTraffic(tx *gorm.DB, emails []string) error {
	if len(emails) == 0 {
		return nil
	}
	type groupDelta struct {
		Name string
		Up   int64
		Down int64
	}
	totals := make(map[string]*groupDelta)
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		var part []groupDelta
		if err := tx.Table("clients AS c").
			Select("c.group_name AS name, COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
			Joins("JOIN client_traffics ct ON ct.email = c.email").
			Where("c.group_name <> '' AND c.email IN ?", batch).
			Group("c.group_name").
			Scan(&part).Error; err != nil {
			return err
		}
		for i := range part {
			if agg, ok := totals[part[i].Name]; ok {
				agg.Up += part[i].Up
				agg.Down += part[i].Down
			} else {
				totals[part[i].Name] = &part[i]
			}
		}
	}
	for name, d := range totals {
		if d.Up == 0 && d.Down == 0 {
			continue
		}
		res := tx.Model(&model.ClientGroup{}).Where("name = ?", name).Updates(map[string]any{
			"reset_up":   gorm.Expr("reset_up - ?", d.Up),
			"reset_down": gorm.Expr("reset_down - ?", d.Down),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Create(&model.ClientGroup{Name: name, ResetUp: -d.Up, ResetDown: -d.Down}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ClientService) EmailsByGroup(name string) ([]string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return []string{}, nil
	}
	db := database.GetDB()
	var emails []string
	if err := db.Model(&model.ClientRecord{}).
		Where("group_name = ?", name).
		Order("email ASC").
		Pluck("email", &emails).Error; err != nil {
		return nil, err
	}
	if emails == nil {
		emails = []string{}
	}
	return emails, nil
}

func (s *ClientService) ResetGroupTraffic(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return common.NewError("group name is required")
	}
	db := database.GetDB()
	var agg struct {
		Up   int64
		Down int64
	}
	if err := db.Table("clients AS c").
		Select("COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
		Joins("LEFT JOIN client_traffics ct ON ct.email = c.email").
		Where("c.group_name = ?", name).
		Scan(&agg).Error; err != nil {
		return err
	}
	var count int64
	if err := db.Model(&model.ClientGroup{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return db.Create(&model.ClientGroup{Name: name, ResetUp: agg.Up, ResetDown: agg.Down}).Error
	}
	return db.Model(&model.ClientGroup{}).Where("name = ?", name).
		Updates(map[string]any{"reset_up": agg.Up, "reset_down": agg.Down}).Error
}

func (s *ClientService) CreateGroup(name string, tariffId *int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return common.NewError("group name is required")
	}
	db := database.GetDB()
	var count int64
	if err := db.Model(&model.ClientGroup{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return common.NewError("group already exists")
	}
	return db.Create(&model.ClientGroup{Name: name, TariffID: tariffId}).Error
}

func (s *ClientService) RenameGroup(oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" {
		return common.NewError("old group name is required")
	}
	if newName == "" {
		return common.NewError("new group name is required")
	}
	if oldName == newName {
		return nil
	}
	return s.replaceGroupValue(oldName, newName)
}

func (s *ClientService) DeleteGroup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return common.NewError("group name is required")
	}
	return s.replaceGroupValue(name, "")
}

func (s *ClientService) RemoveFromGroup(emails []string) error {
	return s.AddToGroup(emails, "")
}

func (s *ClientService) AddToGroup(emails []string, group string) error {
	group = strings.TrimSpace(group)
	if len(emails) == 0 {
		return nil
	}
	db := database.GetDB()

	if group != "" {
		var exists int64
		if err := db.Model(&model.ClientGroup{}).Where("name = ?", group).Count(&exists).Error; err != nil {
			return err
		}
		if exists == 0 {
			var derived int64
			if err := db.Model(&model.ClientRecord{}).Where("group_name = ?", group).Count(&derived).Error; err != nil {
				return err
			}
			if derived == 0 {
				if err := db.Create(&model.ClientGroup{Name: group}).Error; err != nil {
					return err
				}
			}
		}
	}

	var records []model.ClientRecord
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		var rows []model.ClientRecord
		if err := db.Where("email IN ?", batch).Find(&rows).Error; err != nil {
			return err
		}
		records = append(records, rows...)
	}
	if len(records) == 0 {
		return nil
	}
	affectedEmails := make([]string, 0, len(records))
	for _, r := range records {
		affectedEmails = append(affectedEmails, r.Email)
	}

	tx := db.Begin()
	for _, batch := range chunkStrings(affectedEmails, sqlInChunk) {
		if err := tx.Model(&model.ClientRecord{}).
			Where("email IN ?", batch).
			UpdateColumn("group_name", group).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	if group != "" {
		var grp model.ClientGroup
		if err := tx.Where("name = ?", group).First(&grp).Error; err == nil && grp.TariffID != nil {
			activateClientTariffsByEmails(tx, affectedEmails, *grp.TariffID)
		}
	}

	return tx.Commit().Error
}

func (s *ClientService) replaceGroupValue(oldName, newName string) error {
	db := database.GetDB()
	if newName == "" {
		if err := db.Where("name = ?", oldName).Delete(&model.ClientGroup{}).Error; err != nil {
			return err
		}
	} else {
		if err := db.Model(&model.ClientGroup{}).Where("name = ?", oldName).Update("name", newName).Error; err != nil {
			return err
		}
	}

	tx := db.Begin()

	if err := tx.Model(&model.ClientRecord{}).
		Where("group_name = ?", oldName).
		UpdateColumn("group_name", newName).Error; err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}
