package service

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestMergeClientRecord_Semantics(t *testing.T) {
	// applyClientRecordMerge does unconditional overwrites for LimitIP/TotalGB/ExpiryTime.
	// The tariff protection is in SyncInbound, not in the merge function itself.
	t.Run("all numeric fields overwrite unconditionally", func(t *testing.T) {
		row := model.ClientRecord{Email: "merge@x.com", LimitIP: 5, TotalGB: 100, ExpiryTime: 999}
		incoming := model.ClientRecord{LimitIP: 0, TotalGB: 200, ExpiryTime: 888}
		applyClientRecordMerge(&row, &incoming)
		if row.LimitIP != 0 {
			t.Errorf("LimitIP = %d, want 0 (unconditional overwrite from incoming)", row.LimitIP)
		}
		if row.TotalGB != 200 {
			t.Errorf("TotalGB = %d, want 200 (unconditional overwrite)", row.TotalGB)
		}
		if row.ExpiryTime != 888 {
			t.Errorf("ExpiryTime = %d, want 888 (unconditional overwrite)", row.ExpiryTime)
		}
	})

	t.Run("string fields only overwrite when non-empty", func(t *testing.T) {
		row := model.ClientRecord{Email: "merge2@x.com", Group: "old-group", UUID: "old-uuid"}
		incoming := model.ClientRecord{Group: "", UUID: "new-uuid"}
		applyClientRecordMerge(&row, &incoming)
		if row.Group != "old-group" {
			t.Errorf("Group = %q, want old-group (empty incoming should not overwrite)", row.Group)
		}
		if row.UUID != "new-uuid" {
			t.Errorf("UUID = %q, want new-uuid", row.UUID)
		}
	})
}

func TestResolveTariffChain_PartialOverrides(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profile := model.Profile{Name: "partial-p", Traffic: int64Ptr(100), LimitIP: intPtr(5), ExpiryDays: intPtr(30)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "partial-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "partial-group", TariffID: &tariff.Id}
	db.Create(&group)

	startedAt := int64(1700000000000)

	// Only TotalGB is overridden — LimitIP and ExpiryTime should still resolve from tariff.
	client := model.ClientRecord{
		Email:                "partial@x.com",
		Group:                "partial-group",
		TotalGB:              50,
		LimitIP:              1,
		ExpiryTime:           999,
	}
	db.Create(&client)
	gbOverride := int64(50)
	db.Create(&model.ClientTariff{
		ClientID: client.Id, TariffID: tariff.Id, StartedAt: startedAt,
		TotalGBOverride: &gbOverride,
	})

	result := ResolveClientFields(nil, nil, &client)

	// TotalGB: overridden → keep client value.
	if result.TotalGB != 50 {
		t.Errorf("TotalGB = %d, want 50 (overridden)", result.TotalGB)
	}
	// LimitIP: not overridden → tariff value (5).
	if result.LimitIP != 5 {
		t.Errorf("LimitIP = %d, want 5 (tariff)", result.LimitIP)
	}
	// ExpiryTime: not overridden → tariff (startedAt + 30 days).
	wantExpiry := startedAt + 30*86400*1000
	if result.ExpiryTime != wantExpiry {
		t.Errorf("ExpiryTime = %d, want %d (tariff)", result.ExpiryTime, wantExpiry)
	}
}

func TestToClientEffective(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profile := model.Profile{Name: "tce-p", Traffic: int64Ptr(100), LimitIP: intPtr(5)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "tce-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "tce-group", TariffID: &tariff.Id}
	db.Create(&group)

	startedAt := int64(1700000000000)
	rec := model.ClientRecord{
		Email:      "tce@x.com",
		Group:      "tce-group",
		TotalGB:    10,
		LimitIP:    1,
		ExpiryTime: 999,
		Enable:     true,
	}
	db.Create(&rec)
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt})

	client := ToClientEffective(&rec)

	wantTotal := int64(100) * bytesPerGB
	if client.TotalGB != wantTotal {
		t.Errorf("TotalGB = %d, want %d", client.TotalGB, wantTotal)
	}
	if client.LimitIP != 5 {
		t.Errorf("LimitIP = %d, want 5", client.LimitIP)
	}
	if client.Email != "tce@x.com" {
		t.Errorf("Email = %q, want tce@x.com", client.Email)
	}
}

func TestResolveForClient_ErrorPaths(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	svc := &ClientTariffService{}

	t.Run("empty group returns nil context", func(t *testing.T) {
		client := model.ClientRecord{Email: "nogroup@x.com", Group: ""}
		ctx, err := svc.resolveForClient(nil, &client)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if ctx != nil {
			t.Error("expected nil context for empty group")
		}
	})

	t.Run("non-existent group returns nil context", func(t *testing.T) {
		client := model.ClientRecord{Email: "nogroup@x.com", Group: "no-such-group"}
		ctx, err := svc.resolveForClient(nil, &client)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if ctx != nil {
			t.Error("expected nil context for non-existent group")
		}
	})

	t.Run("group without tariff returns nil context", func(t *testing.T) {
		db := database.GetDB()
		db.Create(&model.ClientGroup{Name: "free-group"})

		client := model.ClientRecord{Email: "free@x.com", Group: "free-group"}
		ctx, err := svc.resolveForClient(nil, &client)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if ctx != nil {
			t.Error("expected nil context for group without tariff")
		}
	})
}
