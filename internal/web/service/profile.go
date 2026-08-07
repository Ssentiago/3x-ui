package service

import (
	"errors"
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type ProfileService struct{}

type ProfileSummary struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Traffic     *int64 `json:"traffic"`
	ExpiryDays  *int   `json:"expiryDays"`
	LimitIP     *int   `json:"limitIp"`
	InboundIds  []int  `json:"inboundIds"`
	TariffCount int    `json:"tariffCount"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

func (s *ProfileService) List() ([]ProfileSummary, error) {
	db := database.GetDB()
	var profiles []model.Profile
	if err := db.Order("id asc").Find(&profiles).Error; err != nil {
		return nil, err
	}
	counts := s.batchCountTariffsByProfileId(len(profiles))
	summaries := make([]ProfileSummary, 0, len(profiles))
	for _, p := range profiles {
		inboundIds, _ := parseInboundIds(p.InboundIds)
		summaries = append(summaries, ProfileSummary{
			Id:          p.Id,
			Name:        p.Name,
			Traffic:     p.Traffic,
			ExpiryDays:  p.ExpiryDays,
			LimitIP:     p.LimitIP,
			InboundIds:  inboundIds,
			TariffCount: counts[p.Id],
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		})
	}
	return summaries, nil
}

func (s *ProfileService) Get(id int) (*ProfileSummary, error) {
	var p model.Profile
	if err := database.GetDB().First(&p, id).Error; err != nil {
		return nil, err
	}
	inboundIds, _ := parseInboundIds(p.InboundIds)
	count, _ := s.countTariffsByProfileId(p.Id)
	return &ProfileSummary{
		Id:          p.Id,
		Name:        p.Name,
		Traffic:     p.Traffic,
		ExpiryDays:  p.ExpiryDays,
		LimitIP:     p.LimitIP,
		InboundIds:  inboundIds,
		TariffCount: count,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}, nil
}

func validateProfileInput(name string, traffic *int64, expiryDays *int, limitIP *int) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	if traffic != nil && *traffic < 0 {
		return errors.New("traffic must be non-negative")
	}
	if expiryDays != nil && *expiryDays < 0 {
		return errors.New("expiryDays must be non-negative")
	}
	if limitIP != nil && *limitIP < 0 {
		return errors.New("limitIP must be non-negative")
	}
	return nil
}

func (s *ProfileService) Create(name string, traffic *int64, expiryDays *int, limitIP *int, inboundIds []int) (*ProfileSummary, error) {
	if err := validateProfileInput(name, traffic, expiryDays, limitIP); err != nil {
		return nil, err
	}
	p := model.Profile{
		Name:       name,
		Traffic:    traffic,
		ExpiryDays: expiryDays,
		LimitIP:    limitIP,
	}
	if inboundIds != nil {
		inboundIdsJSON, err := marshalInboundIds(inboundIds)
		if err != nil {
			return nil, err
		}
		p.InboundIds = inboundIdsJSON
	}
	if err := database.GetDB().Create(&p).Error; err != nil {
		return nil, err
	}
	return s.Get(p.Id)
}

func (s *ProfileService) Update(id int, name string, traffic *int64, expiryDays *int, limitIP *int, inboundIds []int) (*ProfileSummary, error) {
	var p model.Profile
	if err := database.GetDB().First(&p, id).Error; err != nil {
		return nil, err
	}
	if err := validateProfileInput(name, traffic, expiryDays, limitIP); err != nil {
		return nil, err
	}
	p.Name = name
	p.Traffic = traffic
	p.ExpiryDays = expiryDays
	p.LimitIP = limitIP
	if inboundIds != nil {
		inboundIdsJSON, err := marshalInboundIds(inboundIds)
		if err != nil {
			return nil, err
		}
		p.InboundIds = inboundIdsJSON
	}
	if err := database.GetDB().Save(&p).Error; err != nil {
		return nil, err
	}
	return s.Get(p.Id)
}

func (s *ProfileService) Delete(id int) error {
	count, err := s.countTariffsByProfileId(id)
	if err != nil {
		return err
	}
	if count > 0 {
		var tariffProfileRows []model.TariffProfile
		database.GetDB().Where("profile_id = ?", id).Find(&tariffProfileRows)
		tariffIDs := make([]int, 0, len(tariffProfileRows))
		for _, tp := range tariffProfileRows {
			tariffIDs = append(tariffIDs, tp.TariffID)
		}
		var tariffs []model.Tariff
		database.GetDB().Where("id IN ?", tariffIDs).Find(&tariffs)
		names := make([]string, len(tariffs))
		for i, t := range tariffs {
			names[i] = t.Name
		}
		return fmt.Errorf("profile is used by tariffs: %v", names)
	}
	return database.GetDB().Delete(&model.Profile{}, id).Error
}

func (s *ProfileService) countTariffsByProfileId(profileId int) (int, error) {
	var count int64
	err := database.GetDB().Model(&model.TariffProfile{}).
		Where("profile_id = ?", profileId).Count(&count).Error
	return int(count), err
}

func (s *ProfileService) batchCountTariffsByProfileId(profileCount int) map[int]int {
	if profileCount == 0 {
		return map[int]int{}
	}
	type row struct {
		ProfileID int
		Count     int
	}
	var rows []row
	database.GetDB().Model(&model.TariffProfile{}).
		Select("profile_id, COUNT(*) as count").
		Group("profile_id").
		Scan(&rows)
	counts := make(map[int]int, len(rows))
	for _, r := range rows {
		counts[r.ProfileID] = r.Count
	}
	return counts
}
