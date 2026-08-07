package sub

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestSubscriptionExpiryFromClient_Edges(t *testing.T) {
	tests := []struct {
		name       string
		nowMs      int64
		expiryTime int64
		want       int64
	}{
		{
			name:       "positive expiry is returned as-is",
			nowMs:      1700000000000,
			expiryTime: 1800000000000,
			want:       1800000000000,
		},
		{
			name:       "zero expiry returns zero",
			nowMs:      1700000000000,
			expiryTime: 0,
			want:       0,
		},
		{
			name:       "negative offset adds to now (delayed start)",
			nowMs:      1700000000000,
			expiryTime: -2592000000, // 30 days in ms
			want:       1700000000000 + 2592000000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := subscriptionExpiryFromClient(tt.nowMs, tt.expiryTime)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAggregateTrafficByEmails_TariffEffectiveLimits(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profile := model.Profile{Name: "agg-p", Traffic: int64Ptr2(100), LimitIP: intPtrSub(5), ExpiryDays: intPtrSub(30)}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile: %v", err)
	}
	tariff := model.Tariff{Name: "agg-t", TrafficStrategy: model.StrategyOverwrite}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatalf("create tariff: %v", err)
	}
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "agg-group", TariffID: &tariff.Id}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	startedAt := int64(1700000000000)

	const email = "tariff-sub@example.com"
	rec := model.ClientRecord{
		Email:      email,
		Group:      "agg-group",
		TotalGB:    10,
		ExpiryTime: 999,
		Enable:     true,
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed client record: %v", err)
	}
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt})
	if err := db.Create(&xray.ClientTraffic{
		Email:      email,
		Up:         100,
		Down:       200,
		Total:      0,
		ExpiryTime: 0,
		Enable:     true,
	}).Error; err != nil {
		t.Fatalf("seed client traffic: %v", err)
	}

	var s SubService
	agg, _ := s.AggregateTrafficByEmails([]string{email})

	wantTotal := int64(100) * (1 << 30)
	if agg.Total != wantTotal {
		t.Errorf("Total = %d, want %d (tariff-resolved 100GB)", agg.Total, wantTotal)
	}

	wantExpiry := startedAt + 30*86400*1000
	if agg.ExpiryTime != wantExpiry {
		t.Errorf("ExpiryTime = %d, want %d (tariff-resolved)", agg.ExpiryTime, wantExpiry)
	}

	if agg.Up != 100 || agg.Down != 200 {
		t.Errorf("usage = up %d/down %d, want 100/200", agg.Up, agg.Down)
	}
}

func int64Ptr2(v int64) *int64 { return &v }
func intPtrSub(v int) *int     { return &v }

func TestAggregateTrafficByEmails_NoTariff_KeepClientLimits(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	const email = "no-tariff-sub@example.com"
	const totalBytes = int64(50) * (1 << 30)
	const expiry = int64(1893456000000)

	if err := db.Create(&model.ClientRecord{
		Email:      email,
		TotalGB:    totalBytes,
		ExpiryTime: expiry,
		Enable:     true,
		Group:      "free-group",
	}).Error; err != nil {
		t.Fatalf("seed client record: %v", err)
	}
	db.Create(&model.ClientGroup{Name: "free-group"})
	if err := db.Create(&xray.ClientTraffic{
		Email:      email,
		Up:         50,
		Down:       50,
		Total:      0,
		ExpiryTime: 0,
		Enable:     true,
	}).Error; err != nil {
		t.Fatalf("seed client traffic: %v", err)
	}

	var s SubService
	agg, _ := s.AggregateTrafficByEmails([]string{email})

	if agg.Total != totalBytes {
		t.Errorf("Total = %d, want %d (client's own limit, no tariff)", agg.Total, totalBytes)
	}
	if agg.ExpiryTime != expiry {
		t.Errorf("ExpiryTime = %d, want %d (client's own expiry, no tariff)", agg.ExpiryTime, expiry)
	}
}

func TestAggregateTrafficByEmails_TariffWithOverrides(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	// Tariff sets 200GB traffic, 90-day expiry. Client overrides totalGB to 50GB.
	profile := model.Profile{Name: "ov-p", Traffic: int64Ptr2(200), ExpiryDays: intPtrSub(90)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "ov-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "ov-group", TariffID: &tariff.Id}
	db.Create(&group)

	startedAt := int64(1700000000000)

	const email = "override-sub@example.com"
	rec := model.ClientRecord{
		Email:      email,
		Group:      "ov-group",
		TotalGB:    50,
		ExpiryTime: 999,
		Enable:     true,
	}
	db.Create(&rec)
	gbOverride := int64(50)
	db.Create(&model.ClientTariff{
		ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt,
		TotalGBOverride: &gbOverride,
	})
	db.Create(&xray.ClientTraffic{
		Email: email, Up: 10, Down: 20, Total: 0, ExpiryTime: 0, Enable: true,
	})

	var s SubService
	agg, _ := s.AggregateTrafficByEmails([]string{email})

	if agg.Total != 50 {
		t.Errorf("Total = %d, want 50 (overridden, not tariff's 200GB)", agg.Total)
	}
	wantExpiry := startedAt + 90*86400*1000
	if agg.ExpiryTime != wantExpiry {
		t.Errorf("ExpiryTime = %d, want %d (tariff-resolved)", agg.ExpiryTime, wantExpiry)
	}
}
