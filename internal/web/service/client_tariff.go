package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"

	"gorm.io/gorm"
)

// ErrNoActiveTariff is returned when OverrideField or ReturnToTariff is
// called for a client without an active tariff row.
var ErrNoActiveTariff = errors.New("client has no active tariff")

type ClientTariffService struct{}

// activateClientTariff ends any previous active tariff row and creates a
// fresh one. The old row's ended_at structurally closes any overrides.
func activateClientTariff(db *gorm.DB, clientId int, tariffId int) {
	if db == nil {
		db = database.GetDB()
	}
	db.Model(&model.ClientTariff{}).
		Where("client_id = ? AND ended_at IS NULL", clientId).
		Update("ended_at", time.Now().UnixMilli())
	db.Create(&model.ClientTariff{
		ClientID:  clientId,
		TariffID:  tariffId,
		StartedAt: time.Now().UnixMilli(),
	})
}

// activateClientTariffsByEmails is the batch version.
func activateClientTariffsByEmails(db *gorm.DB, emails []string, tariffId int) {
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		var ids []int
		db.Model(&model.ClientRecord{}).Where("email IN ?", batch).Pluck("id", &ids)
		if len(ids) == 0 {
			continue
		}
		now := time.Now().UnixMilli()
		db.Model(&model.ClientTariff{}).
			Where("client_id IN ? AND ended_at IS NULL", ids).
			Update("ended_at", now)
		rows := make([]model.ClientTariff, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, model.ClientTariff{
				ClientID: id, TariffID: tariffId, StartedAt: now,
			})
		}
		db.Create(&rows)
	}
}

const bytesPerGB = 1 << 30

// TariffResolved holds the pre-resolved inbound chain for a tariff.
type TariffResolved struct {
	InboundIds []int
	IsUnion    bool
}

// resolveTariffInboundMap loads, resolves and caches tariff inbound chains for
// groups that have a tariff. Returns a map from group name to resolved chain.
// Each tariff is resolved at most once (cache by tariff ID).
func ResolveTariffInboundMap(db *gorm.DB, groups []model.ClientGroup) map[string]TariffResolved {
	if len(groups) == 0 {
		return nil
	}
	cache := make(map[int]TariffResolved)
	result := make(map[string]TariffResolved, len(groups))
	for _, g := range groups {
		tid := *g.TariffID
		if tr, ok := cache[tid]; ok {
			result[g.Name] = tr
			continue
		}
		var t model.Tariff
		if err := db.First(&t, tid).Error; err != nil {
			continue
		}
		var profiles []model.Profile
		db.Table("profiles").
			Joins("JOIN tariff_profiles ON tariff_profiles.profile_id = profiles.id").
			Where("tariff_profiles.tariff_id = ?", tid).
			Order("tariff_profiles.position ASC").
			Find(&profiles)
		ctx := &tariffContext{Tariff: &t, Profiles: profiles}
		chain := resolveChain(ctx)
		tr := TariffResolved{
			InboundIds: chain.InboundIds,
			IsUnion:    t.InboundStrategy == model.StrategyUnion,
		}
		cache[tid] = tr
		result[g.Name] = tr
	}
	return result
}

func (s *ClientTariffService) ApplyInboundList(client *model.ClientRecord, inboundIds []int, inboundSvc *InboundService) (bool, error) {
	if len(inboundIds) == 0 {
		return false, nil
	}

	var clientService ClientService
	currentIds, err := clientService.GetInboundIdsForRecord(client.Id)
	if err != nil {
		return false, err
	}

	currentSet := make(map[int]struct{}, len(currentIds))
	for _, id := range currentIds {
		currentSet[id] = struct{}{}
	}
	tariffSet := make(map[int]struct{}, len(inboundIds))
	for _, id := range inboundIds {
		tariffSet[id] = struct{}{}
	}

	var toAdd, toRemove []int
	for _, id := range inboundIds {
		if _, ok := currentSet[id]; !ok {
			toAdd = append(toAdd, id)
		}
	}
	for _, id := range currentIds {
		if _, ok := tariffSet[id]; !ok {
			toRemove = append(toRemove, id)
		}
	}

	if len(toAdd) == 0 && len(toRemove) == 0 {
		return false, nil
	}

	changed := false
	if len(toRemove) > 0 {
		for _, id := range toRemove {
			if _, err := clientService.DetachByEmail(inboundSvc, id, client.Email); err != nil {
				logger.Warningf("Failed to detach %s from inbound %d: %v", client.Email, id, err)
			} else {
				changed = true
			}
		}
	}

	if len(toAdd) > 0 {
		if _, err := clientService.AttachByEmail(inboundSvc, client.Email, toAdd); err != nil {
			logger.Warningf("Failed to attach %s to inbounds: %v", client.Email, err)
		} else {
			changed = true
		}
	}

	return changed, nil
}

func (s *ClientTariffService) OverrideField(email string, field string) error {
	db := database.GetDB()
	var client model.ClientRecord
	if err := db.Where("email = ?", email).First(&client).Error; err != nil {
		return err
	}
	f := ResolveClientFields(nil, nil, &client)

	var ct model.ClientTariff
	if err := db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct).Error; err != nil {
		return ErrNoActiveTariff
	}

	switch field {
	case "totalGB":
		return db.Model(&ct).Update("total_gb_override", f.TotalGB).Error
	case "limitIP":
		return db.Model(&ct).Update("limit_ip_override", f.LimitIP).Error
	case "expiryTime":
		return db.Model(&ct).Update("expiry_time_override", f.ExpiryTime).Error
	case "inbounds":
		idsJSON, _ := json.Marshal(f.InboundIds)
		return db.Model(&ct).Updates(map[string]any{
			"is_inbounds_overridden": true,
			"inbound_ids_override":   string(idsJSON),
		}).Error
	default:
		return fmt.Errorf("unknown field: %s", field)
	}
}

func (s *ClientTariffService) ReturnToTariff(email string, field string) error {
	db := database.GetDB()
	var client model.ClientRecord
	if err := db.Where("email = ?", email).First(&client).Error; err != nil {
		return err
	}

	var ct model.ClientTariff
	if err := db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct).Error; err != nil {
		return ErrNoActiveTariff
	}

	switch field {
	case "totalGB":
		return db.Model(&ct).Update("total_gb_override", nil).Error
	case "limitIP":
		return db.Model(&ct).Update("limit_ip_override", nil).Error
	case "expiryTime":
		return db.Model(&ct).Update("expiry_time_override", nil).Error
	case "inbounds":
		return db.Model(&ct).Updates(map[string]any{
			"is_inbounds_overridden": false,
			"inbound_ids_override":   nil,
		}).Error
	default:
		return fmt.Errorf("unknown field: %s", field)
	}
}
