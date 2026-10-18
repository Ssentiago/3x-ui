package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/gorm"
)

const policyBytesPerGB = 1 << 30

type PolicyService struct{}

type PolicySummary struct {
	Id         int    `json:"id"`
	Name       string `json:"name"`
	TrafficGB  *int64 `json:"trafficGB"`
	ExpiryDays *int   `json:"expiryDays"`
	LimitIP    *int   `json:"limitIp"`
	InboundIds []int  `json:"inboundIds"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type PolicyInput struct {
	Name       string `json:"name"`
	TrafficGB  *int64 `json:"trafficGB"`
	ExpiryDays *int   `json:"expiryDays"`
	LimitIP    *int   `json:"limitIp"`
	InboundIds []int  `json:"inboundIds"`
}

// ApplyFieldChange describes one field of one client that apply would change.
type ApplyFieldChange struct {
	Email  string `json:"email"`
	Field  string `json:"field"`
	OldVal string `json:"oldVal"`
	NewVal string `json:"newVal"`
}

// ApplyPreview summarizes what an apply would do to a group.
type ApplyPreview struct {
	TotalClients int                `json:"totalClients"`
	Skipped      int                `json:"skipped"`
	Changed      int                `json:"changed"`
	FieldChanges map[string]int     `json:"fieldChanges"`
	Changes      []ApplyFieldChange `json:"changes"`
}

// ApplyResult is the post-apply report.
type ApplyResult struct {
	TotalClients     int            `json:"totalClients"`
	Skipped          int            `json:"skipped"`
	Changed          int            `json:"changed"`
	FieldChanges     map[string]int `json:"fieldChanges"`
	InboundsAttached int            `json:"inboundsAttached"`
	InboundsDetached int            `json:"inboundsDetached"`
}

func policyToSummary(p *model.Policy) PolicySummary {
	return PolicySummary{
		Id:         p.Id,
		Name:       p.Name,
		TrafficGB:  p.TrafficGB,
		ExpiryDays: p.ExpiryDays,
		LimitIP:    p.LimitIP,
		InboundIds: parsePolicyInboundIds(p.InboundIds),
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

func parsePolicyInboundIds(raw string) []int {
	if raw == "" {
		return nil
	}
	var ids []int
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	return ids
}

func marshalPolicyInboundIds(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *PolicyService) List(db *gorm.DB) ([]PolicySummary, error) {
	var rows []model.Policy
	if err := db.Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]PolicySummary, 0, len(rows))
	for i := range rows {
		out = append(out, policyToSummary(&rows[i]))
	}
	return out, nil
}

func (s *PolicyService) Get(db *gorm.DB, id int) (*PolicySummary, error) {
	var p model.Policy
	if err := db.First(&p, id).Error; err != nil {
		return nil, err
	}
	sum := policyToSummary(&p)
	return &sum, nil
}

func validatePolicyInput(in PolicyInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return common.NewError("policy name is required")
	}
	if in.TrafficGB != nil && *in.TrafficGB < 0 {
		return common.NewError("trafficGB must be >= 0")
	}
	if in.ExpiryDays != nil && *in.ExpiryDays < 1 {
		return common.NewError("expiryDays must be >= 1")
	}
	if in.LimitIP != nil && *in.LimitIP < 0 {
		return common.NewError("limitIp must be >= 0")
	}
	return nil
}

func (s *PolicyService) Create(db *gorm.DB, in PolicyInput) (*PolicySummary, error) {
	if err := validatePolicyInput(in); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	var count int64
	db.Model(&model.Policy{}).Where("name = ?", name).Count(&count)
	if count > 0 {
		return nil, common.NewError("policy already exists")
	}
	p := model.Policy{
		Name:       name,
		TrafficGB:  in.TrafficGB,
		ExpiryDays: in.ExpiryDays,
		LimitIP:    in.LimitIP,
		InboundIds: marshalPolicyInboundIds(in.InboundIds),
	}
	if err := db.Create(&p).Error; err != nil {
		return nil, err
	}
	sum := policyToSummary(&p)
	return &sum, nil
}

func (s *PolicyService) Update(db *gorm.DB, id int, in PolicyInput) error {
	if err := validatePolicyInput(in); err != nil {
		return err
	}
	var p model.Policy
	if err := db.First(&p, id).Error; err != nil {
		return err
	}
	name := strings.TrimSpace(in.Name)
	var count int64
	db.Model(&model.Policy{}).Where("name = ? AND id != ?", name, id).Count(&count)
	if count > 0 {
		return common.NewError("policy already exists")
	}
	p.Name = name
	p.TrafficGB = in.TrafficGB
	p.ExpiryDays = in.ExpiryDays
	p.LimitIP = in.LimitIP
	p.InboundIds = marshalPolicyInboundIds(in.InboundIds)
	return db.Save(&p).Error
}

func (s *PolicyService) Delete(db *gorm.DB, id int) error {
	return db.Delete(&model.Policy{}, id).Error
}

// ListExceptions returns the email list excluded from apply for a group.
func ListExceptions(db *gorm.DB, groupName string) ([]string, error) {
	var rows []model.GroupException
	if err := db.Where("group_name = ?", groupName).Order("email ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Email)
	}
	return out, nil
}

// SetExceptions replaces the exception list for a group.
func SetExceptions(db *gorm.DB, groupName string, emails []string) error {
	if strings.TrimSpace(groupName) == "" {
		return common.NewError("group name is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_name = ?", groupName).Delete(&model.GroupException{}).Error; err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(emails))
		for _, e := range emails {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			if _, ok := seen[e]; ok {
				continue
			}
			seen[e] = struct{}{}
			if err := tx.Create(&model.GroupException{GroupName: groupName, Email: e}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AddException adds one email to the group exception list (idempotent).
func AddException(db *gorm.DB, groupName, email string) error {
	groupName = strings.TrimSpace(groupName)
	email = strings.TrimSpace(email)
	if groupName == "" || email == "" {
		return common.NewError("group name and email are required")
	}
	var count int64
	db.Model(&model.GroupException{}).Where("group_name = ? AND email = ?", groupName, email).Count(&count)
	if count > 0 {
		return nil
	}
	return db.Create(&model.GroupException{GroupName: groupName, Email: email}).Error
}

// RemoveException deletes one email from the group exception list.
func RemoveException(db *gorm.DB, groupName, email string) error {
	return db.Where("group_name = ? AND email = ?", groupName, email).Delete(&model.GroupException{}).Error
}

type clientPolicyState struct {
	Email      string
	TotalGB    int64
	ExpiryTime int64
	LimitIP    int
	InboundIds []int
}

func loadClientsForApply(db *gorm.DB, group string) ([]clientPolicyState, error) {
	var rows []model.ClientRecord
	if err := db.Where("group_name = ?", group).Order("email ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	clientSvc := &ClientService{}
	out := make([]clientPolicyState, 0, len(rows))
	for i := range rows {
		ids, err := clientSvc.GetInboundIdsForRecord(rows[i].Id)
		if err != nil {
			ids = nil
		}
		out = append(out, clientPolicyState{
			Email:      rows[i].Email,
			TotalGB:    rows[i].TotalGB,
			ExpiryTime: rows[i].ExpiryTime,
			LimitIP:    rows[i].LimitIP,
			InboundIds: ids,
		})
	}
	return out, nil
}

// policyTarget computes the values a policy writes onto every non-excepted
// client. Nullable scalar fields resolve to 0 (unlimited / no expiry) so
// clearing a field in the policy resets clients, not skips them. Inbounds
// are the exception: empty means "do not touch".
func policyTarget(p *model.Policy, nowMs int64) (
	trafficBytes int64,
	expiryMs int64,
	limitIP int,
	inboundIds []int,
	hasInbounds bool,
) {
	if p.TrafficGB != nil {
		trafficBytes = *p.TrafficGB * policyBytesPerGB
	}
	if p.ExpiryDays != nil {
		expiryMs = nowMs + int64(*p.ExpiryDays)*86400000
	}
	if p.LimitIP != nil {
		limitIP = *p.LimitIP
	}
	if p.InboundIds != "" {
		inboundIds = parsePolicyInboundIds(p.InboundIds)
		hasInbounds = true
	}
	return
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func formatIntSlice(ids []int) string {
	if len(ids) == 0 {
		return "[]"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func diffPolicyClients(
	p *model.Policy,
	clients []clientPolicyState,
	exceptions map[string]struct{},
	nowMs int64,
) (changed []clientPolicyState, fieldChanges map[string]int, changes []ApplyFieldChange, skipped int) {
	trafficBytes, expiryMs, limitIP, inboundIds, hasInbounds := policyTarget(p, nowMs)
	fieldChanges = map[string]int{}
	for _, c := range clients {
		if _, ok := exceptions[c.Email]; ok {
			skipped++
			continue
		}
		mutated := clientPolicyState{
			Email:      c.Email,
			TotalGB:    trafficBytes,
			ExpiryTime: expiryMs,
			LimitIP:    limitIP,
			InboundIds: c.InboundIds,
		}
		touched := false
		if c.TotalGB != trafficBytes {
			fieldChanges["totalGB"]++
			changes = append(changes, ApplyFieldChange{
				Email: c.Email, Field: "totalGB",
				OldVal: common.FormatTraffic(c.TotalGB), NewVal: common.FormatTraffic(trafficBytes),
			})
			touched = true
		}
		if c.ExpiryTime != expiryMs {
			fieldChanges["expiryTime"]++
			changes = append(changes, ApplyFieldChange{
				Email: c.Email, Field: "expiryTime",
				OldVal: formatExpiry(c.ExpiryTime), NewVal: formatExpiry(expiryMs),
			})
			touched = true
		}
		if c.LimitIP != limitIP {
			fieldChanges["limitIP"]++
			changes = append(changes, ApplyFieldChange{
				Email: c.Email, Field: "limitIP",
				OldVal: strconv.Itoa(c.LimitIP), NewVal: strconv.Itoa(limitIP),
			})
			touched = true
		}
		if hasInbounds && !equalIntSlices(c.InboundIds, inboundIds) {
			fieldChanges["inbounds"]++
			changes = append(changes, ApplyFieldChange{
				Email: c.Email, Field: "inbounds",
				OldVal: formatIntSlice(c.InboundIds), NewVal: formatIntSlice(inboundIds),
			})
			touched = true
		}
		if touched {
			changed = append(changed, mutated)
		}
	}
	return
}

func formatExpiry(ms int64) string {
	if ms == 0 {
		return "0"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}

// Preview computes the effect of applying a policy to a group without writing.
func Preview(db *gorm.DB, group string, policyId int) (*ApplyPreview, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil, common.NewError("group name is required")
	}
	var p model.Policy
	if err := db.First(&p, policyId).Error; err != nil {
		return nil, err
	}
	clients, err := loadClientsForApply(db, group)
	if err != nil {
		return nil, err
	}
	exceptionList, err := ListExceptions(db, group)
	if err != nil {
		return nil, err
	}
	exceptions := make(map[string]struct{}, len(exceptionList))
	for _, e := range exceptionList {
		exceptions[e] = struct{}{}
	}
	changed, fieldChanges, changes, skipped := diffPolicyClients(&p, clients, exceptions, time.Now().UnixMilli())
	if changes == nil {
		changes = []ApplyFieldChange{}
	}
	if fieldChanges == nil {
		fieldChanges = map[string]int{}
	}
	return &ApplyPreview{
		TotalClients: len(clients),
		Skipped:      skipped,
		Changed:      len(changed),
		FieldChanges: fieldChanges,
		Changes:      changes,
	}, nil
}

// Apply writes the policy onto non-excepted clients of a group and syncs
// their inbound attachments through the bulk attach/detach path.
func Apply(
	db *gorm.DB,
	clientSvc *ClientService,
	inboundSvc *InboundService,
	group string,
	policyId int,
) (*ApplyResult, bool, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil, false, common.NewError("group name is required")
	}
	var p model.Policy
	if err := db.First(&p, policyId).Error; err != nil {
		return nil, false, err
	}
	clients, err := loadClientsForApply(db, group)
	if err != nil {
		return nil, false, err
	}
	exceptionList, err := ListExceptions(db, group)
	if err != nil {
		return nil, false, err
	}
	exceptions := make(map[string]struct{}, len(exceptionList))
	for _, e := range exceptionList {
		exceptions[e] = struct{}{}
	}
	changed, fieldChanges, _, skipped := diffPolicyClients(&p, clients, exceptions, time.Now().UnixMilli())
	result := &ApplyResult{
		TotalClients: len(clients),
		Skipped:      skipped,
		Changed:      len(changed),
		FieldChanges: fieldChanges,
	}
	if len(changed) == 0 {
		return result, false, nil
	}
	needRestart := false
	for _, c := range changed {
		updates := map[string]any{
			"total_gb":    c.TotalGB,
			"expiry_time": c.ExpiryTime,
			"limit_ip":    c.LimitIP,
		}
		if err := db.Model(&model.ClientRecord{}).Where("email = ?", c.Email).Updates(updates).Error; err != nil {
			return result, needRestart, err
		}
	}
	if p.InboundIds != "" {
		target := parsePolicyInboundIds(p.InboundIds)
		for _, c := range changed {
			if equalIntSlices(c.InboundIds, target) {
				continue
			}
			current := make(map[int]struct{}, len(c.InboundIds))
			for _, id := range c.InboundIds {
				current[id] = struct{}{}
			}
			targetSet := make(map[int]struct{}, len(target))
			for _, id := range target {
				targetSet[id] = struct{}{}
			}
			var toAdd, toRemove []int
			for _, id := range target {
				if _, ok := current[id]; !ok {
					toAdd = append(toAdd, id)
				}
			}
			for _, id := range c.InboundIds {
				if _, ok := targetSet[id]; !ok {
					toRemove = append(toRemove, id)
				}
			}
			if len(toRemove) > 0 {
				_, nr, err := clientSvc.BulkDetach(inboundSvc, []string{c.Email}, toRemove)
				if err != nil {
					return result, needRestart, err
				}
				if nr {
					needRestart = true
				}
				result.InboundsDetached += len(toRemove)
			}
			if len(toAdd) > 0 {
				_, nr, err := clientSvc.BulkAttach(inboundSvc, []string{c.Email}, toAdd)
				if err != nil {
					return result, needRestart, err
				}
				if nr {
					needRestart = true
				}
				result.InboundsAttached += len(toAdd)
			}
		}
	}
	return result, needRestart, nil
}
