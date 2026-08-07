package service

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

func applyClientRecordMerge(row *model.ClientRecord, incoming *model.ClientRecord) {
	if incoming.UUID != "" {
		row.UUID = incoming.UUID
	}
	if incoming.Password != "" {
		row.Password = incoming.Password
	}
	if incoming.Auth != "" {
		row.Auth = incoming.Auth
	}
	if incoming.Secret != "" {
		row.Secret = incoming.Secret
	}
	if incoming.AdTag != "" {
		row.AdTag = incoming.AdTag
	}
	row.Flow = incoming.Flow
	if incoming.Security != "" {
		row.Security = incoming.Security
	}
	if incoming.Reverse != "" {
		row.Reverse = incoming.Reverse
	}
	if incoming.PrivateKey != "" {
		row.PrivateKey = incoming.PrivateKey
	}
	if incoming.PublicKey != "" {
		row.PublicKey = incoming.PublicKey
	}
	if incoming.AllowedIPs != "" {
		row.AllowedIPs = incoming.AllowedIPs
	}
	row.PreSharedKey = incoming.PreSharedKey
	row.KeepAlive = incoming.KeepAlive
	row.SubID = incoming.SubID
	row.LimitIP = incoming.LimitIP
	row.TotalGB = incoming.TotalGB
	row.ExpiryTime = incoming.ExpiryTime
	row.Enable = incoming.Enable
	row.TgID = incoming.TgID
	if incoming.Group != "" {
		row.Group = incoming.Group
	}
	row.Comment = incoming.Comment
	row.Reset = incoming.Reset
	if incoming.CreatedAt > 0 && (row.CreatedAt == 0 || incoming.CreatedAt < row.CreatedAt) {
		row.CreatedAt = incoming.CreatedAt
	}
}

func (s *ClientService) SyncInbound(tx *gorm.DB, inboundId int, clients []model.Client) error {
	if tx == nil {
		tx = database.GetDB()
	}

	if err := tx.Where("inbound_id = ?", inboundId).Delete(&model.ClientInbound{}).Error; err != nil {
		return err
	}

	emails := make([]string, 0, len(clients))
	seen := make(map[string]struct{}, len(clients))
	for i := range clients {
		email := strings.TrimSpace(clients[i].Email)
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}

	existing := make(map[string]*model.ClientRecord, len(emails))
	const selectChunk = 400
	for start := 0; start < len(emails); start += selectChunk {
		end := min(start+selectChunk, len(emails))
		var rows []model.ClientRecord
		if err := tx.Where("email IN ?", emails[start:end]).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			r := rows[i]
			existing[r.Email] = &r
		}
	}

	// Load which groups have tariffs — used below to decide whether a field
	// should be protected from inbound settings JSON writes.
	groupHasTariff := make(map[string]bool)
	activeCT := make(map[int]*model.ClientTariff)
	{
		uniq := make(map[string]struct{})
		for _, r := range existing {
			if r.Group == "" {
				continue
			}
			uniq[r.Group] = struct{}{}
		}
		if len(uniq) > 0 {
			names := make([]string, 0, len(uniq))
			for n := range uniq {
				names = append(names, n)
			}
			var grps []model.ClientGroup
			if err := tx.Where("name IN ? AND tariff_id IS NOT NULL", names).Find(&grps).Error; err != nil {
				return err
			}
			for _, g := range grps {
				groupHasTariff[g.Name] = true
			}
		}
	}
	{
		ids := make([]int, 0, len(existing))
		for _, r := range existing {
			ids = append(ids, r.Id)
		}
		activeCT = GetActiveClientTariffMap(tx, ids)
	}

	idByEmail := make(map[string]int, len(emails))
	pending := make(map[string]*model.ClientRecord, len(emails))
	toCreate := make([]*model.ClientRecord, 0, len(emails))
	for i := range clients {
		email := strings.TrimSpace(clients[i].Email)
		if email == "" {
			continue
		}

		incoming := clients[i].ToRecord()
		// ToRecord copies the raw email; store the trimmed key this function
		// looks up by, or a padded email is inserted and never found again.
		incoming.Email = email
		row, ok := existing[email]
		if !ok {
			if _, dup := pending[email]; !dup {
				pending[email] = incoming
				toCreate = append(toCreate, incoming)
			}
			continue
		}

		before := *row
		applyClientRecordMerge(row, incoming)
		if groupHasTariff[row.Group] {
			act := activeCT[row.Id]
			gbOverridden := act != nil && act.TotalGBOverride != nil
			ipOverridden := act != nil && act.LimitIPOverride != nil
			expOverridden := act != nil && act.ExpiryTimeOverride != nil
			if !gbOverridden {
				row.TotalGB = before.TotalGB
			}
			if !ipOverridden {
				row.LimitIP = before.LimitIP
			}
			if !expOverridden {
				row.ExpiryTime = before.ExpiryTime
			}
		}
		preservedUpdatedAt := max(incoming.UpdatedAt, row.UpdatedAt)
		row.UpdatedAt = preservedUpdatedAt

		idByEmail[email] = row.Id

		if *row == before {
			continue
		}
		if err := tx.Save(row).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ClientRecord{}).
			Where("id = ?", row.Id).
			UpdateColumn("updated_at", preservedUpdatedAt).Error; err != nil {
			return err
		}
	}

	if len(toCreate) > 0 {
		if err := tx.CreateInBatches(toCreate, 200).Error; err != nil {
			return err
		}
		for _, rec := range toCreate {
			idByEmail[rec.Email] = rec.Id
		}
	}

	links := make([]model.ClientInbound, 0, len(clients))
	linked := make(map[int]struct{}, len(clients))
	for i := range clients {
		email := strings.TrimSpace(clients[i].Email)
		if email == "" {
			continue
		}
		id, ok := idByEmail[email]
		if !ok {
			continue
		}
		if _, dup := linked[id]; dup {
			continue
		}
		linked[id] = struct{}{}
		links = append(links, model.ClientInbound{
			ClientId:     id,
			InboundId:    inboundId,
			FlowOverride: clients[i].Flow,
		})
	}
	if len(links) > 0 {
		if err := tx.CreateInBatches(links, 200).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) DetachInbound(tx *gorm.DB, inboundId int) error {
	if tx == nil {
		tx = database.GetDB()
	}
	return tx.Where("inbound_id = ?", inboundId).Delete(&model.ClientInbound{}).Error
}

func (s *ClientService) ListForInbound(tx *gorm.DB, inboundId int) ([]model.Client, error) {
	return s.listForInboundFiltered(tx, inboundId, "")
}

func (s *ClientService) ListForInboundBySubId(tx *gorm.DB, inboundId int, subId string) ([]model.Client, error) {
	return s.listForInboundFiltered(tx, inboundId, subId)
}

func (s *ClientService) listForInboundFiltered(tx *gorm.DB, inboundId int, subId string) ([]model.Client, error) {
	if tx == nil {
		tx = database.GetDB()
	}
	type joinedRow struct {
		model.ClientRecord
		FlowOverride string
	}
	hasSub := subId != ""

	// Direct clients: own inbounds + union-strategy tariff clients (keep
	// direct attachments). Clients of deleted tariffs fall back to ownIds.
	var direct []joinedRow
	q1 := tx.Table("clients").
		Select("clients.*, client_inbounds.flow_override AS flow_override").
		Joins("JOIN client_inbounds ON client_inbounds.client_id = clients.id").
		Where("client_inbounds.inbound_id = ?", inboundId).
		Where(`(
			EXISTS (SELECT 1 FROM client_tariffs ct_ov WHERE ct_ov.client_id = clients.id AND ct_ov.ended_at IS NULL AND ct_ov.is_inbounds_overridden = TRUE)
			OR clients.group_name = ''
			OR NOT EXISTS (
				SELECT 1 FROM client_groups cg
				JOIN tariffs t ON t.id = cg.tariff_id
				WHERE cg.name = clients.group_name
			)
			OR EXISTS (
				SELECT 1 FROM client_groups cg
				JOIN tariffs t ON t.id = cg.tariff_id
				WHERE cg.name = clients.group_name
				AND t.inbound_strategy = 'union'
			)
		)`)
	if hasSub {
		q1 = q1.Where("clients.sub_id = ?", subId)
	}
	if err := q1.Order("clients.id ASC").Find(&direct).Error; err != nil {
		return nil, err
	}

	// Step 2: Tariff-resolved clients.
	tariffIds := s.tariffIdsContainingInbound(tx, inboundId)
	var tariff []joinedRow
	if len(tariffIds) > 0 {
		var groupNames []string
		if err := tx.Model(&model.ClientGroup{}).
			Where("tariff_id IN ?", tariffIds).
			Pluck("name", &groupNames).Error; err != nil {
			return nil, err
		}
		if len(groupNames) > 0 {
			q2 := tx.Table("clients").
				Select("clients.*, client_inbounds.flow_override AS flow_override").
				Joins("LEFT JOIN client_inbounds ON client_inbounds.client_id = clients.id AND client_inbounds.inbound_id = ?", inboundId).
				Where("clients.group_name IN ? AND NOT EXISTS (SELECT 1 FROM client_tariffs ct_ov2 WHERE ct_ov2.client_id = clients.id AND ct_ov2.ended_at IS NULL AND ct_ov2.is_inbounds_overridden = TRUE)", groupNames)
			if hasSub {
				q2 = q2.Where("clients.sub_id = ?", subId)
			}
			if err := q2.Order("clients.id ASC").Find(&tariff).Error; err != nil {
				return nil, err
			}
		}
	}

	all := make([]joinedRow, 0, len(direct)+len(tariff))
	all = append(all, direct...)
	all = append(all, tariff...)

	seen := make(map[string]bool, len(all))
	var unique []joinedRow
	for _, r := range all {
		if !seen[r.Email] {
			seen[r.Email] = true
			unique = append(unique, r)
		}
	}

	out := make([]model.Client, 0, len(unique))
	for i := range unique {
		rec := &unique[i].ClientRecord
		f := ResolveClientLimits(nil, rec)
		c := rec.ToClientEffective(f.LimitIP, f.TotalGB, f.ExpiryTime)
		c.Flow = unique[i].FlowOverride
		out = append(out, *c)
	}
	return out, nil
}

// tariffIdsContainingInbound returns the IDs of tariffs whose resolved inbound
// chain includes the given inboundId.
func (s *ClientService) tariffIdsContainingInbound(tx *gorm.DB, inboundId int) []int {
	var rows []struct {
		TariffID int
	}
	tx.Table("tariff_profiles tp").
		Select("DISTINCT tp.tariff_id").
		Joins("JOIN profiles p ON p.id = tp.profile_id").
		Where("p.inbound_ids IS NOT NULL AND p.inbound_ids != '' AND p.inbound_ids != 'null'").
		Scan(&rows)
	if len(rows) == 0 {
		return nil
	}
	tariffIDs := make(map[int]struct{})
	for _, r := range rows {
		tariffIDs[r.TariffID] = struct{}{}
	}
	tariffKeys := make([]int, 0, len(tariffIDs))
	for k := range tariffIDs {
		tariffKeys = append(tariffKeys, k)
	}

	// Batch-load all profiles for all candidate tariffs in one query.
	var allProfiles []struct {
		TariffID int
		Profile  model.Profile `gorm:"embedded"`
	}
	tx.Table("profiles").
		Select("tariff_profiles.tariff_id, profiles.*").
		Joins("JOIN tariff_profiles ON tariff_profiles.profile_id = profiles.id").
		Where("tariff_profiles.tariff_id IN ?", tariffKeys).
		Order("tariff_profiles.tariff_id, tariff_profiles.position ASC").
		Scan(&allProfiles)

	tariffProfiles := make(map[int][]model.Profile, len(tariffIDs))
	for _, ap := range allProfiles {
		tariffProfiles[ap.TariffID] = append(tariffProfiles[ap.TariffID], ap.Profile)
	}

	var tariffs []model.Tariff
	tx.Where("id IN ?", tariffKeys).Find(&tariffs)
	var result []int
	for _, t := range tariffs {
		ctx := &tariffContext{Tariff: &t, Profiles: tariffProfiles[t.Id]}
		chain := resolveChain(ctx)
		for _, id := range chain.InboundIds {
			if id == inboundId {
				result = append(result, t.Id)
				break
			}
		}
	}
	return result
}
