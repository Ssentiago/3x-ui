package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

func TestResolveChain(t *testing.T) {
	tests := []struct {
		name     string
		tariff   model.Tariff
		profiles []model.Profile
		want     EffectiveConfig
	}{
		{
			name:   "empty chain gives zero config",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite, InboundStrategy: model.StrategyOverwrite},
			want:   EffectiveConfig{},
		},
		{
			name:   "single profile sets traffic with overwrite",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{Traffic: int64Ptr(10)},
			},
			want: EffectiveConfig{Traffic: 10 * bytesPerGB},
		},
		{
			name:   "single profile sets traffic with sum",
			tariff: model.Tariff{TrafficStrategy: model.StrategySum},
			profiles: []model.Profile{
				{Traffic: int64Ptr(5)},
				{Traffic: int64Ptr(3)},
			},
			want: EffectiveConfig{Traffic: 8 * bytesPerGB},
		},
		{
			name:   "last profile wins with overwrite for traffic",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{Traffic: int64Ptr(10)},
				{Traffic: int64Ptr(5)},
			},
			want: EffectiveConfig{Traffic: 5 * bytesPerGB},
		},
		{
			name:   "null profile fields do not overwrite",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{Traffic: int64Ptr(10), LimitIP: intPtr(3)},
				{Traffic: nil, LimitIP: nil},
			},
			want: EffectiveConfig{Traffic: 10 * bytesPerGB, LimitIP: 3},
		},
		{
			name:   "expiry from last non-null profile",
			tariff: model.Tariff{},
			profiles: []model.Profile{
				{ExpiryDays: intPtr(30)},
				{ExpiryDays: intPtr(7)},
			},
			want: EffectiveConfig{ExpiryDays: 7},
		},
		{
			name:   "limit_ip from the last profile that sets it",
			tariff: model.Tariff{},
			profiles: []model.Profile{
				{LimitIP: intPtr(5)},
				{LimitIP: intPtr(2)},
			},
			want: EffectiveConfig{LimitIP: 2},
		},
		{
			name:   "inbound overwrite takes last profile",
			tariff: model.Tariff{InboundStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{InboundIds: "[1, 2]"},
				{InboundIds: "[3]"},
			},
			want: EffectiveConfig{InboundIds: []int{3}},
		},
		{
			name:   "inbound union deduplicates",
			tariff: model.Tariff{InboundStrategy: model.StrategyUnion},
			profiles: []model.Profile{
				{InboundIds: "[1, 2]"},
				{InboundIds: "[2, 3]"},
			},
			want: EffectiveConfig{InboundIds: []int{1, 2, 3}},
		},
		{
			name:   "inbound null value is skipped",
			tariff: model.Tariff{InboundStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{InboundIds: "[1, 2]"},
				{InboundIds: "null"},
			},
			want: EffectiveConfig{InboundIds: []int{1, 2}},
		},
		{
			name:   "mixed fields across profiles, overwrite for traffic",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{Traffic: int64Ptr(10), ExpiryDays: intPtr(30)},
				{LimitIP: intPtr(3), InboundIds: "[1]"},
			},
			want: EffectiveConfig{Traffic: 10 * bytesPerGB, ExpiryDays: 30, LimitIP: 3, InboundIds: []int{1}},
		},
		{
			name:   "sum strategy with three profiles",
			tariff: model.Tariff{TrafficStrategy: model.StrategySum},
			profiles: []model.Profile{
				{Traffic: int64Ptr(5)},
				{Traffic: int64Ptr(3)},
				{Traffic: int64Ptr(2)},
			},
			want: EffectiveConfig{Traffic: 10 * bytesPerGB},
		},
		{
			name:   "empty string inboundIds skipped",
			tariff: model.Tariff{InboundStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{InboundIds: "[1, 2]"},
				{InboundIds: ""},
			},
			want: EffectiveConfig{InboundIds: []int{1, 2}},
		},
		{
			name:   "nil profile between two non-nil profiles",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{Traffic: int64Ptr(10), LimitIP: intPtr(5)},
				{Traffic: nil, LimitIP: nil},
				{},
			},
			want: EffectiveConfig{Traffic: 10 * bytesPerGB, LimitIP: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &tariffContext{Tariff: &tt.tariff, Profiles: tt.profiles}
			got := resolveChain(ctx)
			if got.Traffic != tt.want.Traffic {
				t.Errorf("Traffic = %d, want %d", got.Traffic, tt.want.Traffic)
			}
			if got.ExpiryDays != tt.want.ExpiryDays {
				t.Errorf("ExpiryDays = %d, want %d", got.ExpiryDays, tt.want.ExpiryDays)
			}
			if got.LimitIP != tt.want.LimitIP {
				t.Errorf("LimitIP = %d, want %d", got.LimitIP, tt.want.LimitIP)
			}
			if len(got.InboundIds) != len(tt.want.InboundIds) {
				t.Fatalf("InboundIds len = %d, want %d", len(got.InboundIds), len(tt.want.InboundIds))
			}
			for i := range got.InboundIds {
				if got.InboundIds[i] != tt.want.InboundIds[i] {
					t.Errorf("InboundIds[%d] = %d, want %d", i, got.InboundIds[i], tt.want.InboundIds[i])
				}
			}
		})
	}
}

func TestResolveChainHasFlags(t *testing.T) {
	tests := []struct {
		name     string
		tariff   model.Tariff
		profiles []model.Profile
		want     EffectiveConfig
	}{
		{
			name:     "empty profiles — no Has* flags",
			tariff:   model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: nil,
			want:     EffectiveConfig{},
		},
		{
			name:   "profile with traffic=0 still sets HasTraffic",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{Traffic: int64Ptr(0)},
			},
			want: EffectiveConfig{HasTraffic: true},
		},
		{
			name:   "profile with nil traffic — HasTraffic stays false",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{Traffic: nil},
			},
			want: EffectiveConfig{},
		},
		{
			name:   "profile with expiry_days=0 — HasExpiryDays=true",
			tariff: model.Tariff{},
			profiles: []model.Profile{
				{ExpiryDays: intPtr(0)},
			},
			want: EffectiveConfig{HasExpiryDays: true},
		},
		{
			name:   "profile with nil expiry_days — HasExpiryDays stays false",
			tariff: model.Tariff{},
			profiles: []model.Profile{
				{ExpiryDays: nil},
			},
			want: EffectiveConfig{},
		},
		{
			name:   "profile with limit_ip=0 — HasLimitIP=true",
			tariff: model.Tariff{},
			profiles: []model.Profile{
				{LimitIP: intPtr(0)},
			},
			want: EffectiveConfig{HasLimitIP: true},
		},
		{
			name:   "profile with nil limit_ip — HasLimitIP stays false",
			tariff: model.Tariff{},
			profiles: []model.Profile{
				{LimitIP: nil},
			},
			want: EffectiveConfig{},
		},
		{
			name:   "two profiles, second resets — first Has* survive",
			tariff: model.Tariff{TrafficStrategy: model.StrategyOverwrite},
			profiles: []model.Profile{
				{Traffic: int64Ptr(10), ExpiryDays: intPtr(30)},
				{Traffic: nil, ExpiryDays: nil},
			},
			want: EffectiveConfig{Traffic: 10 * bytesPerGB, ExpiryDays: 30, HasTraffic: true, HasExpiryDays: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &tariffContext{Tariff: &tt.tariff, Profiles: tt.profiles}
			got := resolveChain(ctx)
			if got.HasTraffic != tt.want.HasTraffic {
				t.Errorf("HasTraffic = %v, want %v", got.HasTraffic, tt.want.HasTraffic)
			}
			if got.HasExpiryDays != tt.want.HasExpiryDays {
				t.Errorf("HasExpiryDays = %v, want %v", got.HasExpiryDays, tt.want.HasExpiryDays)
			}
			if got.HasLimitIP != tt.want.HasLimitIP {
				t.Errorf("HasLimitIP = %v, want %v", got.HasLimitIP, tt.want.HasLimitIP)
			}
		})
	}
}

func TestMergeInboundIds(t *testing.T) {
	tests := []struct {
		name  string
		own   []int
		chain []int
		want  []int
	}{
		{"empty both", nil, nil, nil},
		{"only own", []int{1, 2}, nil, []int{1, 2}},
		{"only chain", nil, []int{3, 4}, []int{3, 4}},
		{"disjoint", []int{1, 2}, []int{3, 4}, []int{1, 2, 3, 4}},
		{"overlap dedup", []int{1, 2, 3}, []int{2, 3, 4}, []int{1, 2, 3, 4}},
		{"fully overlapped", []int{1, 2}, []int{1, 2}, []int{1, 2}},
		{"unsorted input, sorted output", []int{5, 3}, []int{4, 1}, []int{1, 3, 4, 5}},
		{"single element each", []int{1}, []int{2}, []int{1, 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeInboundIds(tt.own, tt.chain)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d, got=%v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestResolveClientFields_NoTariff verifies raw values when no tariff.
func TestResolveClientFields_NoTariff(t *testing.T) {
	db, cleanup := initTestDB(t)
	defer cleanup()
	rec := model.ClientRecord{Email: "no-tariff@test", TotalGB: 50, LimitIP: 3, ExpiryTime: 1700000000000}
	db.Create(&rec)
	got := ResolveClientFields(nil, nil, &rec)
	if got.TotalGB != 50 || got.LimitIP != 3 || got.ExpiryTime != 1700000000000 {
		t.Errorf("got {TotalGB=%d, LimitIP=%d, ExpiryTime=%d}, want {50, 3, 1700000000000}", got.TotalGB, got.LimitIP, got.ExpiryTime)
	}
}

// TestResolveClientFields_Tariff verifies tariff-resolved values.
func TestResolveClientFields_Tariff(t *testing.T) {
	db, cleanup := initTestDB(t)
	defer cleanup()
	p := model.Profile{Name: "p", Traffic: int64Ptr(20), LimitIP: intPtr(5)}
	db.Create(&p)
	tariff := model.Tariff{Name: "t1", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})
	grp := model.ClientGroup{Name: "g1", TariffID: &tariff.Id}
	db.Create(&grp)
	rec := model.ClientRecord{Email: "tariff@test", Group: "g1", TotalGB: 50, LimitIP: 3, ExpiryTime: 1700000000000}
	db.Create(&rec)
	got := ResolveClientFields(nil, nil, &rec)
	if got.TotalGB != 20*bytesPerGB || got.LimitIP != 5 {
		t.Errorf("got {TotalGB=%d, LimitIP=%d}, want {%d, 5}", got.TotalGB, got.LimitIP, 20*bytesPerGB)
	}
}

// TestResolveClientFields_Override verifies override beats tariff.
func TestResolveClientFields_Override(t *testing.T) {
	db, cleanup := initTestDB(t)
	defer cleanup()
	p := model.Profile{Name: "p2", Traffic: int64Ptr(20), LimitIP: intPtr(5)}
	db.Create(&p)
	tariff := model.Tariff{Name: "t2"}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})
	grp := model.ClientGroup{Name: "g2", TariffID: &tariff.Id}
	db.Create(&grp)
	rec := model.ClientRecord{
		Email: "override@test", Group: "g2", TotalGB: 100, LimitIP: 10, ExpiryTime: 1700000000000,
	}
	db.Create(&rec)
	gbOverride := int64(100)
	ipOverride := 10
	db.Create(&model.ClientTariff{
		ClientID: rec.Id, TariffID: tariff.Id, StartedAt: 1700000000000,
		TotalGBOverride: &gbOverride, LimitIPOverride: &ipOverride,
	})
	got := ResolveClientFields(nil, nil, &rec)
	if got.TotalGB != 100 || got.LimitIP != 10 {
		t.Errorf("got {TotalGB=%d, LimitIP=%d}, want {100, 10}", got.TotalGB, got.LimitIP)
	}
}

// TestResolveClientFields_Expiry verifies tariff expiry with ClientTariff record.
func TestResolveClientFields_Expiry(t *testing.T) {
	db, cleanup := initTestDB(t)
	defer cleanup()
	p := model.Profile{Name: "p3", ExpiryDays: intPtr(30)}
	db.Create(&p)
	tariff := model.Tariff{Name: "t3"}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})
	grp := model.ClientGroup{Name: "g3", TariffID: &tariff.Id}
	db.Create(&grp)
	started := int64(1700000000000)
	rec := model.ClientRecord{Email: "expiry@test", Group: "g3", ExpiryTime: 0}
	db.Create(&rec)
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: started})
	got := ResolveClientFields(nil, nil, &rec)
	want := started + 30*86400*1000
	if got.ExpiryTime != want {
		t.Errorf("ExpiryTime = %d, want %d", got.ExpiryTime, want)
	}
}

func initTestDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	_ = database.CloseDB()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	return database.GetDB(), func() { _ = database.CloseDB() }
}

func TestEffectiveFunctions(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profile := model.Profile{Name: "test-profile", Traffic: int64Ptr(10), LimitIP: intPtr(5)}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile: %v", err)
	}

	tariff := model.Tariff{Name: "test-tariff", TrafficStrategy: model.StrategyOverwrite}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatalf("create tariff: %v", err)
	}

	tp := model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0}
	if err := db.Create(&tp).Error; err != nil {
		t.Fatalf("create tariff_profile: %v", err)
	}

	group := model.ClientGroup{Name: "test-group", TariffID: &tariff.Id}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	clientRec := model.ClientRecord{
		Email:      "test@example.com",
		Group:      "test-group",
		TotalGB:    50,
		LimitIP:    3,
		ExpiryTime: 1700000000000,
	}
	if err := db.Create(&clientRec).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	t.Run("ResolveClientFields limitIP through tariff chain", func(t *testing.T) {
		limit := ResolveClientFields(nil, nil, &clientRec).LimitIP
		if limit != 5 {
			t.Errorf("EffectiveLimitIP = %d, want 5", limit)
		}
	})

	t.Run("ResolveClientFields totalGB through tariff chain", func(t *testing.T) {
		gb := ResolveClientFields(nil, nil, &clientRec).TotalGB
		want := int64(10) * bytesPerGB
		if gb != want {
			t.Errorf("EffectiveTotalGB = %d, want %d", gb, want)
		}
	})

	t.Run("ResolveClientFields expiryTime returns client value when tariff has no expiry", func(t *testing.T) {
		expiry := ResolveClientFields(nil, nil, &clientRec).ExpiryTime
		if expiry != 1700000000000 {
			t.Errorf("EffectiveExpiryTime = %d, want 1700000000000", expiry)
		}
	})

	t.Run("ResolveClientFields limitIP with override", func(t *testing.T) {
		db.Exec("UPDATE client_tariffs SET ended_at = ? WHERE client_id = ? AND ended_at IS NULL", time.Now().UnixMilli(), clientRec.Id)
		ipOverride := 7
		db.Create(&model.ClientTariff{
			ClientID: clientRec.Id, TariffID: tariff.Id, StartedAt: 1700000000000,
			LimitIPOverride: &ipOverride,
		})
		defer db.Exec("UPDATE client_tariffs SET ended_at = ? WHERE client_id = ? AND ended_at IS NULL", time.Now().UnixMilli(), clientRec.Id)
		limit := ResolveClientFields(nil, nil, &clientRec).LimitIP
		if limit != 7 {
			t.Errorf("EffectiveLimitIP with override = %d, want 7", limit)
		}
	})

	t.Run("ResolveClientFields totalGB with override", func(t *testing.T) {
		db.Exec("UPDATE client_tariffs SET ended_at = ? WHERE client_id = ? AND ended_at IS NULL", time.Now().UnixMilli(), clientRec.Id)
		gbOverride := int64(200)
		db.Create(&model.ClientTariff{
			ClientID: clientRec.Id, TariffID: tariff.Id, StartedAt: 1700000000001,
			TotalGBOverride: &gbOverride,
		})
		defer db.Exec("UPDATE client_tariffs SET ended_at = ? WHERE client_id = ? AND ended_at IS NULL", time.Now().UnixMilli(), clientRec.Id)
		gb := ResolveClientFields(nil, nil, &clientRec).TotalGB
		if gb != 200 {
			t.Errorf("EffectiveTotalGB with override = %d, want 200", gb)
		}
	})

	t.Run("no tariff group returns client defaults", func(t *testing.T) {
		noGroup := model.ClientRecord{Email: "nogroup@example.com", TotalGB: 10, LimitIP: 1}
		db.Create(&noGroup)
		limit := ResolveClientFields(nil, nil, &noGroup).LimitIP
		if limit != 1 {
			t.Errorf("EffectiveLimitIP no group = %d, want 1", limit)
		}
	})
}

// Verify sqlEffTotalGB, sqlEffExpiry, sqlEffLimitIP produce the same results as
// the Go resolver on identical data.
func TestSqlEffectiveMatchesGoResolver(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	baseProfile := model.Profile{Name: "base", Traffic: int64Ptr(10), ExpiryDays: intPtr(30), LimitIP: intPtr(3)}
	extraProfile := model.Profile{Name: "extra", Traffic: int64Ptr(20), LimitIP: intPtr(5)}
	noTrafficProfile := model.Profile{Name: "notraffic", ExpiryDays: intPtr(7)}

	for _, p := range []*model.Profile{&baseProfile, &extraProfile, &noTrafficProfile} {
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("create profile %s: %v", p.Name, err)
		}
	}

	overwriteTariff := model.Tariff{Name: "overwrite-tariff", TrafficStrategy: model.StrategyOverwrite}
	sumTariff := model.Tariff{Name: "sum-tariff", TrafficStrategy: model.StrategySum}
	for _, tr := range []*model.Tariff{&overwriteTariff, &sumTariff} {
		if err := db.Create(tr).Error; err != nil {
			t.Fatalf("create tariff %s: %v", tr.Name, err)
		}
	}

	for _, tp := range []model.TariffProfile{
		{TariffID: overwriteTariff.Id, ProfileID: baseProfile.Id, Position: 0},
		{TariffID: overwriteTariff.Id, ProfileID: extraProfile.Id, Position: 1},
		{TariffID: sumTariff.Id, ProfileID: baseProfile.Id, Position: 2},
		{TariffID: sumTariff.Id, ProfileID: extraProfile.Id, Position: 3},
	} {
		if err := db.Create(&tp).Error; err != nil {
			t.Fatalf("create tariff_profile: %v", err)
		}
	}

	owGroup := model.ClientGroup{Name: "ow-group", TariffID: &overwriteTariff.Id}
	sumGroup := model.ClientGroup{Name: "sum-group", TariffID: &sumTariff.Id}
	noTariffGroup := model.ClientGroup{Name: "notariff-group"}
	for _, g := range []*model.ClientGroup{&owGroup, &sumGroup, &noTariffGroup} {
		if err := db.Create(g).Error; err != nil {
			t.Fatalf("create group %s: %v", g.Name, err)
		}
	}

	startedAt := int64(1700000000000)

	owClient := model.ClientRecord{
		Email: "ow@test.com", Group: "ow-group",
		TotalGB: 100, LimitIP: 10, ExpiryTime: 999,
	}
	sumClient := model.ClientRecord{
		Email: "sum@test.com", Group: "sum-group",
		TotalGB: 100, LimitIP: 10, ExpiryTime: 999,
	}
	overriddenClient := model.ClientRecord{
		Email: "overridden@test.com", Group: "ow-group",
		TotalGB: 5, LimitIP: 1, ExpiryTime: 999,
	}
	noTariffClient := model.ClientRecord{
		Email: "notariff@test.com", Group: "notariff-group",
		TotalGB: 50, LimitIP: 2, ExpiryTime: 1700000000000,
	}
	noGroupClient := model.ClientRecord{
		Email:   "nogroup@test.com",
		TotalGB: 25, LimitIP: 1, ExpiryTime: 0,
	}

	for _, cl := range []*model.ClientRecord{&owClient, &sumClient, &overriddenClient, &noTariffClient, &noGroupClient} {
		if err := db.Create(cl).Error; err != nil {
			t.Fatalf("create client %s: %v", cl.Email, err)
		}
	}

	// Create active tariff entries for the three tariff-bound clients.
	for _, ct := range []struct {
		clientId, tariffId int
	}{
		{owClient.Id, overwriteTariff.Id},
		{sumClient.Id, sumTariff.Id},
	} {
		db.Create(&model.ClientTariff{
			ClientID: ct.clientId, TariffID: ct.tariffId, StartedAt: startedAt,
		})
	}
	gbOverride := int64(5)
	ipOverride := 1
	expOverride := int64(999)
	db.Create(&model.ClientTariff{
		ClientID: overriddenClient.Id, TariffID: overwriteTariff.Id, StartedAt: startedAt,
		TotalGBOverride: &gbOverride, LimitIPOverride: &ipOverride, ExpiryTimeOverride: &expOverride,
	})

	type sqlRow struct {
		Email   string
		TotalGB int64
		Expiry  int64
		LimitIP int
	}
	var sqlRows []sqlRow
	query := `SELECT c.email,
		` + sqlEffTotalGB + ` AS total_gb,
		` + sqlEffExpiry + ` AS expiry,
		` + sqlEffLimitIP + ` AS limit_ip
		FROM clients c
		LEFT JOIN client_groups cgr ON cgr.name = c.group_name
		LEFT JOIN client_tariffs cta ON cta.client_id = c.id AND cta.ended_at IS NULL
		ORDER BY c.email`
	if err := db.Raw(query).Scan(&sqlRows).Error; err != nil {
		t.Fatalf("sql query: %v", err)
	}

	for _, row := range sqlRows {
		var rec model.ClientRecord
		if err := db.Where("email = ?", row.Email).First(&rec).Error; err != nil {
			t.Fatalf("fetch client %s: %v", row.Email, err)
		}

		f := ResolveClientFields(nil, nil, &rec)
		goTotal := f.TotalGB
		goExpiry := f.ExpiryTime
		goLimit := f.LimitIP

		if row.TotalGB != goTotal {
			t.Errorf("%s sqlEffTotalGB = %d, EffectiveTotalGB = %d", row.Email, row.TotalGB, goTotal)
		}
		if row.Expiry != goExpiry {
			t.Errorf("%s sqlEffExpiry = %d, EffectiveExpiryTime = %d", row.Email, row.Expiry, goExpiry)
		}
		if row.LimitIP != goLimit {
			t.Errorf("%s sqlEffLimitIP = %d, EffectiveLimitIP = %d", row.Email, row.LimitIP, goLimit)
		}
	}
}

// SQL returns NULL without an active client_tariffs row; Go returns raw
// ExpiryTime. The test locks this gap so both sides stay aware of it.
func TestSqlExpiryNullWithoutStartedAt(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profile := model.Profile{Name: "nullat-p", ExpiryDays: intPtr(30)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "nullat-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "nullat-group", TariffID: &tariff.Id}
	db.Create(&group)

	// Client in tariff group but NO ClientTariff row.
	client := model.ClientRecord{
		Email:      "nullat@test.com",
		Group:      "nullat-group",
		ExpiryTime: 1700000000000,
	}
	db.Create(&client)

	// Go resolver: returns raw ExpiryTime (no started_at → no override of ExpiryTime).
	goResult := ResolveClientFields(nil, nil, &client)
	if goResult.ExpiryTime != 1700000000000 {
		t.Errorf("Go ExpiryTime = %d, want 1700000000000 (raw fallback)", goResult.ExpiryTime)
	}
	// TariffExpiryDays is still populated from the chain.
	if goResult.TariffExpiryDays != 30 {
		t.Errorf("TariffExpiryDays = %d, want 30", goResult.TariffExpiryDays)
	}

	// SQL expression: returns NULL because client_tariffs subquery returns NULL.
	var sqlExpiry struct {
		Expiry *int64
	}
	query := `SELECT ` + sqlEffExpiry + ` AS expiry FROM clients c
		LEFT JOIN client_groups cgr ON cgr.name = c.group_name
		WHERE c.email = 'nullat@test.com'`
	db.Raw(query).Scan(&sqlExpiry)

	if sqlExpiry.Expiry != nil {
		t.Errorf("SQL sqlEffExpiry = %d, want NULL (no started_at → subquery returns NULL)", *sqlExpiry.Expiry)
	}
}
