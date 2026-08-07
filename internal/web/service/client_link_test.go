package service

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestSyncInboundTariffProtection(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	inbound := model.Inbound{Tag: "sync-tag", Port: 12345, Protocol: "vmess", Settings: "{}", StreamSettings: "{}"}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	profile := model.Profile{Name: "sync-p", Traffic: int64Ptr(100), LimitIP: intPtr(5), ExpiryDays: intPtr(30)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "sync-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "sync-group", TariffID: &tariff.Id}
	db.Create(&group)

	startedAt := int64(1700000000000)
	rec := model.ClientRecord{
		Email:      "sync-protect@x.com",
		Group:      "sync-group",
		TotalGB:    50,
		LimitIP:    3,
		ExpiryTime: 1700000000000,
		Enable:     true,
	}
	db.Create(&rec)
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt})
	db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: inbound.Id})

	svc := &ClientService{}

	t.Run("tariff fields are restored after merge when not overridden", func(t *testing.T) {
		// Incoming settings have different values for all three tariff fields
		incoming := model.Client{
			Email:      "sync-protect@x.com",
			TotalGB:    999,
			LimitIP:    99,
			ExpiryTime: 999999,
			Enable:     true,
		}

		if err := svc.SyncInbound(nil, inbound.Id, []model.Client{incoming}); err != nil {
			t.Fatalf("SyncInbound: %v", err)
		}

		var updated model.ClientRecord
		db.Where("email = ?", "sync-protect@x.com").First(&updated)

		if updated.TotalGB != 50 {
			t.Errorf("TotalGB = %d, want 50 (protected from JSON)", updated.TotalGB)
		}
		if updated.LimitIP != 3 {
			t.Errorf("LimitIP = %d, want 3 (protected from JSON)", updated.LimitIP)
		}
		if updated.ExpiryTime != 1700000000000 {
			t.Errorf("ExpiryTime = %d, want 1700000000000 (protected from JSON)", updated.ExpiryTime)
		}
	})

	t.Run("overridden fields keep incoming JSON values", func(t *testing.T) {
		gbOverride := int64(777)
		ipOverride := 77
		expOverride := int64(777777)
		db.Model(&model.ClientTariff{}).
			Where("client_id = ? AND ended_at IS NULL", rec.Id).
			Updates(map[string]any{
				"total_gb_override":    &gbOverride,
				"limit_ip_override":    &ipOverride,
				"expiry_time_override": &expOverride,
			})

		incoming := model.Client{
			Email:      "sync-protect@x.com",
			TotalGB:    777,
			LimitIP:    77,
			ExpiryTime: 777777,
			Enable:     true,
		}

		if err := svc.SyncInbound(nil, inbound.Id, []model.Client{incoming}); err != nil {
			t.Fatalf("SyncInbound: %v", err)
		}

		var updated model.ClientRecord
		db.Where("email = ?", "sync-protect@x.com").First(&updated)

		if updated.TotalGB != 777 {
			t.Errorf("TotalGB = %d, want 777 (overridden, accept JSON)", updated.TotalGB)
		}
		if updated.LimitIP != 77 {
			t.Errorf("LimitIP = %d, want 77 (overridden, accept JSON)", updated.LimitIP)
		}
		if updated.ExpiryTime != 777777 {
			t.Errorf("ExpiryTime = %d, want 777777 (overridden, accept JSON)", updated.ExpiryTime)
		}
	})
}

func TestSyncInbound_NonTariffClient(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	inbound := model.Inbound{Tag: "sync-nt", Port: 12346, Protocol: "vmess", Settings: "{}", StreamSettings: "{}"}
	db.Create(&inbound)

	rec := model.ClientRecord{
		Email:      "sync-notariff@x.com",
		TotalGB:    10,
		LimitIP:    1,
		ExpiryTime: 100,
		Enable:     true,
	}
	db.Create(&rec)
	db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: inbound.Id})

	svc := &ClientService{}

	t.Run("non-tariff client accepts incoming JSON values", func(t *testing.T) {
		incoming := model.Client{
			Email:      "sync-notariff@x.com",
			TotalGB:    888,
			LimitIP:    88,
			ExpiryTime: 888888,
			Enable:     true,
		}

		if err := svc.SyncInbound(nil, inbound.Id, []model.Client{incoming}); err != nil {
			t.Fatalf("SyncInbound: %v", err)
		}

		var updated model.ClientRecord
		db.Where("email = ?", "sync-notariff@x.com").First(&updated)

		if updated.TotalGB != 888 {
			t.Errorf("TotalGB = %d, want 888 (no tariff, accept JSON)", updated.TotalGB)
		}
		if updated.LimitIP != 88 {
			t.Errorf("LimitIP = %d, want 88 (no tariff, accept JSON)", updated.LimitIP)
		}
		if updated.ExpiryTime != 888888 {
			t.Errorf("ExpiryTime = %d, want 888888 (no tariff, accept JSON)", updated.ExpiryTime)
		}
	})
}

func TestTariffIdsContainingInbound(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profileWithInbound := model.Profile{Name: "tic-p1", InboundIds: "[10,20]"}
	profileNoInbound := model.Profile{Name: "tic-p2"}
	db.Create(&profileWithInbound)
	db.Create(&profileNoInbound)

	tariffWith := model.Tariff{Name: "tic-has", InboundStrategy: model.StrategyOverwrite}
	tariffWithout := model.Tariff{Name: "tic-no", InboundStrategy: model.StrategyOverwrite}
	db.Create(&tariffWith)
	db.Create(&tariffWithout)
	db.Create(&model.TariffProfile{TariffID: tariffWith.Id, ProfileID: profileWithInbound.Id, Position: 0})
	db.Create(&model.TariffProfile{TariffID: tariffWithout.Id, ProfileID: profileNoInbound.Id, Position: 5})

	svc := &ClientService{}

	t.Run("tariff containing inbound 10 is found", func(t *testing.T) {
		ids := svc.tariffIdsContainingInbound(db, 10)
		found := false
		for _, id := range ids {
			if id == tariffWith.Id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tariff %d should contain inbound 10, got %v", tariffWith.Id, ids)
		}
	})

	t.Run("tariff without inbound 50 is not found", func(t *testing.T) {
		ids := svc.tariffIdsContainingInbound(db, 50)
		for _, id := range ids {
			if id == tariffWith.Id {
				t.Errorf("tariff %d should not contain inbound 50", tariffWith.Id)
			}
		}
	})

	t.Run("non-existent inbound returns empty", func(t *testing.T) {
		ids := svc.tariffIdsContainingInbound(db, 99999)
		if len(ids) != 0 {
			t.Errorf("expected empty, got %v", ids)
		}
	})

	t.Run("tariff without inbound profile is excluded", func(t *testing.T) {
		ids := svc.tariffIdsContainingInbound(db, 10)
		for _, id := range ids {
			if id == tariffWithout.Id {
				t.Errorf("tariff %d has no inbound profile, should not match", tariffWithout.Id)
			}
		}
	})
}
