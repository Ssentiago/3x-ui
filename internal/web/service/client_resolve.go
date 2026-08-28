package service

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"

	"gorm.io/gorm"
)

type tariffContext struct {
	Tariff   *model.Tariff
	Profiles []model.Profile
}

type EffectiveConfig struct {
	Traffic       int64
	ExpiryDays    int
	LimitIP       int
	InboundIds    []int
	HasTraffic    bool
	HasExpiryDays bool
	HasLimitIP    bool
}

func resolveChain(ctx *tariffContext) *EffectiveConfig {
	cfg := &EffectiveConfig{}
	for _, p := range ctx.Profiles {
		if p.Traffic != nil {
			cfg.HasTraffic = true
			val := *p.Traffic * bytesPerGB
			if ctx.Tariff.TrafficStrategy == model.StrategySum {
				cfg.Traffic += val
			} else {
				cfg.Traffic = val
			}
		}
		if p.ExpiryDays != nil {
			cfg.HasExpiryDays = true
			cfg.ExpiryDays = *p.ExpiryDays
		}
		if p.LimitIP != nil {
			cfg.HasLimitIP = true
			cfg.LimitIP = *p.LimitIP
		}
		if p.InboundIds != "" && p.InboundIds != "null" {
			ids, _ := parseInboundIds(p.InboundIds)
			if ctx.Tariff.InboundStrategy == model.StrategyUnion {
				seen := make(map[int]struct{})
				for _, id := range cfg.InboundIds {
					seen[id] = struct{}{}
				}
				for _, id := range ids {
					if _, ok := seen[id]; !ok {
						cfg.InboundIds = append(cfg.InboundIds, id)
						seen[id] = struct{}{}
					}
				}
			} else {
				cfg.InboundIds = append([]int(nil), ids...)
			}
		}
	}
	return cfg
}

// ChainPreviewResult is the response for POST /tariffs/preview. Includes
// source profile names for each resolved field so the UI can show which
// profile contributed the value.
type ChainPreviewResult struct {
	TrafficBytes  int64  `json:"trafficBytes"`
	ExpiryDays    int    `json:"expiryDays"`
	LimitIP       int    `json:"limitIp"`
	InboundIds    []int  `json:"inboundIds"`
	TrafficSource string `json:"trafficSource"`
	ExpirySource  string `json:"expirySource"`
	IPSource      string `json:"ipSource"`
	InboundSource string `json:"inboundSource"`
}

type ChainProfilePreview struct {
	Name       *string `json:"name"`
	Traffic    *int64  `json:"traffic"`
	ExpiryDays *int    `json:"expiryDays"`
	LimitIP    *int    `json:"limitIp"`
	InboundIds []int   `json:"inboundIds"`
}

func ResolveChainPreview(profiles []ChainProfilePreview, trafficStrategy, inboundStrategy string) *ChainPreviewResult {
	var trafficBytes int64
	var expiryDays, limitIP int
	var inboundIds []int
	var trafficSource, expirySource, ipSource, inboundSource string

	for _, p := range profiles {
		name := ""
		if p.Name != nil {
			name = *p.Name
		}
		if p.Traffic != nil {
			val := *p.Traffic * bytesPerGB
			if trafficStrategy == model.StrategySum {
				trafficBytes += val
				if trafficSource != "" {
					trafficSource += " + "
				}
				trafficSource += name
			} else {
				trafficBytes = val
				trafficSource = name
			}
		}
		if p.ExpiryDays != nil {
			expiryDays = *p.ExpiryDays
			expirySource = name
		}
		if p.LimitIP != nil {
			limitIP = *p.LimitIP
			ipSource = name
		}
		if len(p.InboundIds) > 0 {
			if inboundStrategy == model.StrategyUnion {
				seen := make(map[int]struct{})
				for _, id := range inboundIds {
					seen[id] = struct{}{}
				}
				for _, id := range p.InboundIds {
					if _, ok := seen[id]; !ok {
						inboundIds = append(inboundIds, id)
						seen[id] = struct{}{}
					}
				}
			} else {
				inboundIds = append([]int(nil), p.InboundIds...)
			}
			inboundSource = name
		}
	}
	return &ChainPreviewResult{
		TrafficBytes:  trafficBytes,
		ExpiryDays:    expiryDays,
		LimitIP:       limitIP,
		InboundIds:    inboundIds,
		TrafficSource: trafficSource,
		ExpirySource:  expirySource,
		IPSource:      ipSource,
		InboundSource: inboundSource,
	}
}

func (s *ClientTariffService) resolveForClient(db *gorm.DB, client *model.ClientRecord) (*tariffContext, error) {
	if client.Group == "" {
		return nil, nil
	}
	if db == nil {
		db = database.GetDB()
	}
	var grp model.ClientGroup
	if err := db.Where("name = ?", client.Group).First(&grp).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Warningf("resolveForClient: group lookup failed for %s: %v", client.Email, err)
		}
		return nil, nil
	}
	if grp.TariffID == nil {
		return nil, nil
	}
	var tariff model.Tariff
	if err := db.First(&tariff, *grp.TariffID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Warningf("resolveForClient: tariff lookup failed for %s (tariffId=%d): %v", client.Email, *grp.TariffID, err)
		}
		return nil, nil
	}
	var profiles []model.Profile
	db.Table("profiles").
		Joins("JOIN tariff_profiles ON tariff_profiles.profile_id = profiles.id").
		Where("tariff_profiles.tariff_id = ?", tariff.Id).
		Order("tariff_profiles.position ASC").
		Find(&profiles)
	return &tariffContext{Tariff: &tariff, Profiles: profiles}, nil
}

func ToClientEffective(rec *model.ClientRecord) *model.Client {
	f := ResolveClientLimits(nil, rec)
	return rec.ToClientEffective(f.LimitIP, f.TotalGB, f.ExpiryTime)
}

// applyOverrides applies active CT row overrides to out. Returns which fields
// were overridden so callers can skip tariff-chain resolution for those fields.
func applyOverrides(active *model.ClientTariff, out *ResolvedClientFields) (gbOv, ipOv, expOv bool) {
	if active == nil {
		return
	}
	if active.TotalGBOverride != nil {
		out.TotalGB = *active.TotalGBOverride
		gbOv = true
	}
	if active.LimitIPOverride != nil {
		out.LimitIP = *active.LimitIPOverride
		ipOv = true
	}
	if active.ExpiryTimeOverride != nil {
		out.ExpiryTime = *active.ExpiryTimeOverride
		expOv = true
	}
	return
}

// parseInboundIDsOverride parses the inbound_ids_override JSON column.
// Returns nil when the column is empty or unparseable.
func parseInboundIDsOverride(active *model.ClientTariff) []int {
	if active.InboundIDsOverride == nil || *active.InboundIDsOverride == "" {
		return nil
	}
	var ids []int
	if err := json.Unmarshal([]byte(*active.InboundIDsOverride), &ids); err != nil {
		logger.Warningf("parseInboundIDsOverride: invalid JSON for client_tariff %d: %v", active.ID, err)
		return nil
	}
	return ids
}

// ClientEffective is the response for GET /client/:email/effective. It
// bundles the active tariff metadata, per-field override flags, and the
// fully resolved values so the frontend can render the edit-form card with
// one request. Nil for clients without an active tariff.
type ClientEffective struct {
	TariffID             int    `json:"tariffId"`
	TariffName           string `json:"tariffName"`
	StartedAt            int64  `json:"startedAt"`
	EndedAt              *int64 `json:"endedAt"`
	IsTotalGBOverridden  bool   `json:"isTotalGBOverridden"`
	IsLimitIPOverridden  bool   `json:"isLimitIPOverridden"`
	IsExpiryOverridden   bool   `json:"isExpiryOverridden"`
	IsInboundsOverridden bool   `json:"isInboundsOverridden"`
	ResolvedTotalGB      int64  `json:"resolvedTotalGB"`
	ResolvedLimitIP      int    `json:"resolvedLimitIP"`
	ResolvedExpiryTime   int64  `json:"resolvedExpiryTime"`
	ResolvedInboundIds   []int  `json:"resolvedInboundIds"`
}

// ResolvedForGroup is the effective values a client would have if placed in a
// group with a tariff.
type ResolvedForGroup struct {
	TotalGB    int64 `json:"totalGB"`
	ExpiryTime int64 `json:"expiryTime"`
	LimitIP    int   `json:"limitIp"`
	InboundIds []int `json:"inboundIds"`
}

// ResolveForGroup resolves what a client's effective values would be if placed
// in the given group. Returns nil if the group has no tariff.
func (s *ClientService) ResolveForGroup(email, group string) (*ResolvedForGroup, error) {
	db := database.GetDB()
	rec, err := s.GetRecordByEmail(db, email)
	if err != nil {
		return nil, err
	}
	saved := rec.Group
	rec.Group = group
	defer func() { rec.Group = saved }()

	active := getActiveClientTariff(db, rec.Id)
	resolved := ResolveClientFields(db, active, rec)

	// Preview expiry: tariff hasn't started yet, so show the duration as a
	// negative offset (e.g. -2592000000 = 30 days from when tariff activates).
	expiryOverridden := active != nil && active.ExpiryTimeOverride != nil
	if !expiryOverridden && resolved.TariffExpiryDays > 0 && active == nil {
		resolved.ExpiryTime = -int64(resolved.TariffExpiryDays) * 86400 * 1000
	}

	return &ResolvedForGroup{
		TotalGB:    resolved.TotalGB,
		ExpiryTime: resolved.ExpiryTime,
		LimitIP:    resolved.LimitIP,
		InboundIds: resolved.InboundIds,
	}, nil
}

// ResolvedClientFields holds all 4 tariff-controlled fields resolved in a
// single pass. Own (raw) values are returned when there is no tariff, the field
// is overridden, or resolution fails. TariffExpiryDays is the chain's raw
// expiry duration — 0 means the tariff has no expiry profile.
type ResolvedClientFields struct {
	TotalGB          int64
	ExpiryTime       int64
	LimitIP          int
	InboundIds       []int
	TariffExpiryDays int
}

// getActiveClientTariff returns the client's active (ended_at IS NULL)
// client_tariffs row, or nil if none exists.
func getActiveClientTariff(db *gorm.DB, clientId int) *model.ClientTariff {
	var ct model.ClientTariff
	if err := db.Where("client_id = ? AND ended_at IS NULL", clientId).First(&ct).Error; err != nil {
		return nil
	}
	return &ct
}

// GetActiveClientTariffMap batch-loads active client_tariffs rows for
// multiple client IDs. Missing entries mean no active tariff.
func GetActiveClientTariffMap(db *gorm.DB, clientIds []int) map[int]*model.ClientTariff {
	result := make(map[int]*model.ClientTariff, len(clientIds))
	for _, batch := range chunkInts(clientIds, sqlInChunk) {
		var rows []model.ClientTariff
		db.Where("client_id IN ? AND ended_at IS NULL", batch).Find(&rows)
		for i := range rows {
			result[rows[i].ClientID] = &rows[i]
		}
	}
	return result
}

// MergeInboundIds merges own inbound IDs with tariff chain IDs, deduplicates,
// and returns a sorted result.
func MergeInboundIds(ownIds, chainIds []int) []int {
	seen := make(map[int]struct{}, len(ownIds)+len(chainIds))
	merged := make([]int, 0, len(ownIds)+len(chainIds))
	for _, id := range ownIds {
		seen[id] = struct{}{}
		merged = append(merged, id)
	}
	for _, id := range chainIds {
		if _, ok := seen[id]; !ok {
			merged = append(merged, id)
		}
	}
	sort.Ints(merged)
	return merged
}

// resolveTariffChain loads the tariff chain for a client and applies chain
// values for fields not overridden on the active client_tariffs row. Does NOT
// resolve inboundIds. Returns the resolved chain and context, or nil if no
// tariff applies.
func resolveTariffChain(db *gorm.DB, active *model.ClientTariff, client *model.ClientRecord, out *ResolvedClientFields) (*tariffContext, *EffectiveConfig) {
	if client.Group == "" {
		return nil, nil
	}
	allOverridden := active != nil &&
		active.TotalGBOverride != nil &&
		active.LimitIPOverride != nil &&
		active.ExpiryTimeOverride != nil
	if allOverridden {
		return nil, nil
	}
	var grp model.ClientGroup
	if err := db.Where("name = ?", client.Group).First(&grp).Error; err != nil || grp.TariffID == nil {
		return nil, nil
	}
	var cts ClientTariffService
	ctx, _ := cts.resolveForClient(db, client)
	if ctx == nil {
		return nil, nil
	}
	chain := resolveChain(ctx)
	out.TariffExpiryDays = chain.ExpiryDays

	totalGBOverridden := active != nil && active.TotalGBOverride != nil
	limitIPOverridden := active != nil && active.LimitIPOverride != nil
	expiryOverridden := active != nil && active.ExpiryTimeOverride != nil

	if !totalGBOverridden && chain.HasTraffic {
		out.TotalGB = chain.Traffic
	}
	if !limitIPOverridden && chain.HasLimitIP {
		out.LimitIP = chain.LimitIP
	}
	if !expiryOverridden && chain.HasExpiryDays && active != nil {
		out.ExpiryTime = active.StartedAt + int64(chain.ExpiryDays)*86400*1000
	}
	return ctx, chain
}

// ResolveClientLimits resolves totalGB, expiryTime, and limitIP through the
// tariff chain. InboundIds is NOT resolved (skips the client_inbounds query).
func ResolveClientLimits(db *gorm.DB, client *model.ClientRecord) ResolvedClientFields {
	if db == nil {
		db = database.GetDB()
	}
	active := getActiveClientTariff(db, client.Id)
	out := ResolvedClientFields{
		TotalGB:    client.TotalGB,
		ExpiryTime: client.ExpiryTime,
		LimitIP:    client.LimitIP,
	}
	applyOverrides(active, &out)
	resolveTariffChain(db, active, client, &out)
	return out
}

// ResolveClientFields resolves all 4 tariff-controlled fields for a client.
// Pass db=nil to use the default connection. Pass active=nil when not
// pre-loaded; a per-call lookup is done otherwise.
func ResolveClientFields(db *gorm.DB, active *model.ClientTariff, client *model.ClientRecord) ResolvedClientFields {
	if db == nil {
		db = database.GetDB()
	}
	if active == nil && client.Group != "" {
		active = getActiveClientTariff(db, client.Id)
	}

	var ownIds []int
	db.Table("client_inbounds").
		Where("client_id = ?", client.Id).
		Order("inbound_id ASC").
		Pluck("inbound_id", &ownIds)

	out := ResolvedClientFields{
		TotalGB:    client.TotalGB,
		ExpiryTime: client.ExpiryTime,
		LimitIP:    client.LimitIP,
		InboundIds: ownIds,
	}

	applyOverrides(active, &out)

	ctx, chain := resolveTariffChain(db, active, client, &out)

	inboundsOverridden := active != nil && active.IsInboundsOverridden
	if inboundsOverridden {
		if ids := parseInboundIDsOverride(active); ids != nil {
			out.InboundIds = ids
		}
		return out
	}
	if ctx == nil {
		return out
	}

	if ctx.Tariff.InboundStrategy == model.StrategyUnion {
		out.InboundIds = MergeInboundIds(ownIds, chain.InboundIds)
	} else {
		out.InboundIds = chain.InboundIds
	}

	return out
}

// TariffConfig holds pre-resolved effective values for one tariff, used by
// ClientBatchResolver to resolve many clients without N+1 DB queries.
type TariffConfig struct {
	Traffic    int64
	ExpiryDays int
	LimitIP    int
	HasTraffic bool
	HasExpiry  bool
	HasLimitIP bool
}

// ClientBatchResolver pre-loads tariff chains and active client_tariffs rows
// for a set of clients so every per-client ResolveLimits call is O(1) with no
// DB queries.
type ClientBatchResolver struct {
	configs  map[string]*TariffConfig
	activeCT map[int]*model.ClientTariff
}

func NewBatchResolver(db *gorm.DB, byId map[int]*model.ClientRecord) *ClientBatchResolver {
	if db == nil {
		db = database.GetDB()
	}
	r := &ClientBatchResolver{activeCT: make(map[int]*model.ClientTariff)}
	groupSet := make(map[string]struct{})
	ids := make([]int, 0, len(byId))
	for id, rec := range byId {
		ids = append(ids, id)
		if rec.Group != "" {
			groupSet[rec.Group] = struct{}{}
		}
	}
	if len(groupSet) == 0 {
		return r
	}
	groupList := make([]string, 0, len(groupSet))
	for g := range groupSet {
		groupList = append(groupList, g)
	}
	var groups []model.ClientGroup
	db.Where("name IN ? AND tariff_id IS NOT NULL", groupList).Find(&groups)

	tariffByGroup := make(map[string]int)
	tariffSet := make(map[int]struct{})
	for _, g := range groups {
		tid := *g.TariffID
		tariffSet[tid] = struct{}{}
		tariffByGroup[g.Name] = tid
	}
	configs := make(map[string]*TariffConfig, len(tariffSet))
	for tid := range tariffSet {
		var t model.Tariff
		if db.First(&t, tid).Error != nil {
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
		for gName, gTid := range tariffByGroup {
			if gTid == tid {
				configs[gName] = &TariffConfig{
					Traffic:    chain.Traffic,
					ExpiryDays: chain.ExpiryDays,
					LimitIP:    chain.LimitIP,
					HasTraffic: chain.HasTraffic,
					HasExpiry:  chain.HasExpiryDays,
					HasLimitIP: chain.HasLimitIP,
				}
			}
		}
	}
	r.configs = configs
	r.activeCT = GetActiveClientTariffMap(db, ids)
	return r
}

func (r *ClientBatchResolver) ResolveLimits(rec *model.ClientRecord) ResolvedClientFields {
	out := ResolvedClientFields{
		TotalGB:    rec.TotalGB,
		ExpiryTime: rec.ExpiryTime,
		LimitIP:    rec.LimitIP,
	}
	if r == nil {
		return out
	}
	active := r.activeCT[rec.Id]
	if active != nil {
		_, _, expOv := applyOverrides(active, &out)
		if expOv {
			return out
		}
	}
	cfg := r.configs[rec.Group]
	totalGBOverridden := active != nil && active.TotalGBOverride != nil
	limitIPOverridden := active != nil && active.LimitIPOverride != nil
	expiryOverridden := active != nil && active.ExpiryTimeOverride != nil
	if !totalGBOverridden && cfg != nil && cfg.HasTraffic {
		out.TotalGB = cfg.Traffic
	}
	if !limitIPOverridden && cfg != nil && cfg.HasLimitIP {
		out.LimitIP = cfg.LimitIP
	}
	if !expiryOverridden && cfg != nil && cfg.HasExpiry && active != nil {
		out.ExpiryTime = active.StartedAt + int64(cfg.ExpiryDays)*86400*1000
	}
	return out
}

// GetEffective returns the active tariff metadata, per-field override flags,
// and fully resolved values for a client. Returns nil when the client has no
// active tariff.
func (s *ClientService) GetEffective(db *gorm.DB, email string) (*ClientEffective, error) {
	if db == nil {
		db = database.GetDB()
	}
	rec, err := s.GetRecordByEmail(db, email)
	if err != nil {
		return nil, err
	}
	var ctRow model.ClientTariff
	if err := db.Where("client_id = ? AND ended_at IS NULL", rec.Id).First(&ctRow).Error; err != nil {
		return nil, nil
	}
	resolved := ResolveClientFields(db, &ctRow, rec)

	var cts ClientTariffService
	ctx, _ := cts.resolveForClient(db, rec)
	if ctx == nil {
		return nil, nil
	}

	return &ClientEffective{
		TariffID:             ctx.Tariff.Id,
		TariffName:           ctx.Tariff.Name,
		StartedAt:            ctRow.StartedAt,
		EndedAt:              ctRow.EndedAt,
		IsTotalGBOverridden:  ctRow.TotalGBOverride != nil,
		IsLimitIPOverridden:  ctRow.LimitIPOverride != nil,
		IsExpiryOverridden:   ctRow.ExpiryTimeOverride != nil,
		IsInboundsOverridden: ctRow.IsInboundsOverridden,
		ResolvedTotalGB:      resolved.TotalGB,
		ResolvedLimitIP:      resolved.LimitIP,
		ResolvedExpiryTime:   resolved.ExpiryTime,
		ResolvedInboundIds:   resolved.InboundIds,
	}, nil
}

// BatchResolveEffectiveInboundIds resolves inbound IDs using batch DB queries.
func BatchResolveEffectiveInboundIds(db *gorm.DB, records []model.ClientRecord) map[int]struct{} {
	if len(records) == 0 {
		return nil
	}
	ids := make([]int, len(records))
	byId := make(map[int]*model.ClientRecord, len(records))
	for i := range records {
		ids[i] = records[i].Id
		byId[records[i].Id] = &records[i]
	}

	type pair struct{ ClientId, InboundId int }
	var pairs []pair
	db.Table("client_inbounds").Where("client_id IN ?", ids).Order("inbound_id ASC").Scan(&pairs)
	ownMap := make(map[int][]int, len(records))
	for _, p := range pairs {
		ownMap[p.ClientId] = append(ownMap[p.ClientId], p.InboundId)
	}

	activeCT := GetActiveClientTariffMap(db, ids)
	resolved := resolveEffectiveInboundsForPage(db, byId, ownMap, activeCT)

	seen := make(map[int]struct{})
	for _, ids := range resolved {
		for _, id := range ids {
			seen[id] = struct{}{}
		}
	}
	return seen
}

// resolveEffectiveInboundsForPage batch-resolves effective inbound IDs for a
// page of clients, merging tariff-resolved inbound chains when applicable.
func resolveEffectiveInboundsForPage(db *gorm.DB, records map[int]*model.ClientRecord, ownInbounds map[int][]int, activeCT map[int]*model.ClientTariff) map[int][]int {
	groupNames := make(map[string]struct{})
	for _, rec := range records {
		active := activeCT[rec.Id]
		if rec.Group != "" && (active == nil || !active.IsInboundsOverridden) {
			groupNames[rec.Group] = struct{}{}
		}
	}
	if len(groupNames) == 0 {
		return ownInbounds
	}

	names := make([]string, 0, len(groupNames))
	for n := range groupNames {
		names = append(names, n)
	}

	var groups []model.ClientGroup
	if err := db.Where("name IN ? AND tariff_id IS NOT NULL", names).Find(&groups).Error; err != nil {
		return ownInbounds
	}

	groupResolved := ResolveTariffInboundMap(db, groups)

	result := make(map[int][]int, len(records))
	for id, rec := range records {
		own := ownInbounds[id]
		tr, hasTariff := groupResolved[rec.Group]
		overridden := activeCT[rec.Id] != nil && activeCT[rec.Id].IsInboundsOverridden
		if overridden || !hasTariff || len(tr.InboundIds) == 0 {
			result[id] = own
			continue
		}
		if tr.IsUnion {
			result[id] = MergeInboundIds(own, tr.InboundIds)
		} else {
			result[id] = tr.InboundIds
		}
	}
	return result
}

// resolveEffectiveTraffic resolves a single client's effective totalGB and
// expiryTime for traffic-stat writes. rawTotalGB/rawExpiry come from the
// inbound settings JSON and serve as fallbacks when the resolver has no
// tariff-provided or overridden value.
func resolveEffectiveTraffic(tx *gorm.DB, email string, rawTotalGB, rawExpiry int64) (int64, int64) {
	var client model.ClientRecord
	if err := tx.Where("email = ?", email).First(&client).Error; err != nil {
		return rawTotalGB, rawExpiry
	}
	active := getActiveClientTariff(tx, client.Id)
	gbOverridden := active != nil && active.TotalGBOverride != nil
	expOverridden := active != nil && active.ExpiryTimeOverride != nil
	if client.Group != "" && (!gbOverridden || !expOverridden) {
		f := ResolveClientLimits(tx, &client)
		if !gbOverridden {
			rawTotalGB = f.TotalGB
		}
		if !expOverridden {
			rawExpiry = f.ExpiryTime
		}
		return rawTotalGB, rawExpiry
	}
	if gbOverridden {
		rawTotalGB = *active.TotalGBOverride
	}
	if expOverridden {
		rawExpiry = *active.ExpiryTimeOverride
	}
	return rawTotalGB, rawExpiry
}
