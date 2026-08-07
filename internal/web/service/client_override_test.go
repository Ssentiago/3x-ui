package service

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestOverrideField(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	profile := model.Profile{Name: "override-p", Traffic: int64Ptr(50), LimitIP: intPtr(5), ExpiryDays: intPtr(30)}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile: %v", err)
	}
	tariff := model.Tariff{Name: "override-t", TrafficStrategy: model.StrategyOverwrite}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatalf("create tariff: %v", err)
	}
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "override-group", TariffID: &tariff.Id}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	startedAt := int64(1700000000000)
	client := model.ClientRecord{
		Email:      "override@x.com",
		Group:      "override-group",
		TotalGB:    10,
		LimitIP:    2,
		ExpiryTime: 999,
	}
	db.Create(&client)
	db.Create(&model.ClientTariff{ClientID: client.Id, TariffID: tariff.Id, StartedAt: startedAt})

	svc := &ClientTariffService{}

	t.Run("override totalGB writes to CT row", func(t *testing.T) {
		if err := svc.OverrideField("override@x.com", "totalGB"); err != nil {
			t.Fatalf("OverrideField totalGB: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		wantTotal := int64(50) * bytesPerGB
		if ct.TotalGBOverride == nil || *ct.TotalGBOverride != wantTotal {
			t.Errorf("TotalGBOverride = %v, want %d", ct.TotalGBOverride, wantTotal)
		}
	})

	t.Run("override limitIP writes to CT row", func(t *testing.T) {
		if err := svc.OverrideField("override@x.com", "limitIP"); err != nil {
			t.Fatalf("OverrideField limitIP: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		if ct.LimitIPOverride == nil || *ct.LimitIPOverride != 5 {
			t.Errorf("LimitIPOverride = %v, want 5", ct.LimitIPOverride)
		}
	})

	t.Run("override expiryTime writes to CT row", func(t *testing.T) {
		if err := svc.OverrideField("override@x.com", "expiryTime"); err != nil {
			t.Fatalf("OverrideField expiryTime: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		wantExpiry := startedAt + 30*86400*1000
		if ct.ExpiryTimeOverride == nil || *ct.ExpiryTimeOverride != wantExpiry {
			t.Errorf("ExpiryTimeOverride = %v, want %d", ct.ExpiryTimeOverride, wantExpiry)
		}
	})

	t.Run("override inbounds sets flag on CT row", func(t *testing.T) {
		if err := svc.OverrideField("override@x.com", "inbounds"); err != nil {
			t.Fatalf("OverrideField inbounds: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		if !ct.IsInboundsOverridden {
			t.Error("IsInboundsOverridden should be true")
		}
	})

	t.Run("unknown field returns error", func(t *testing.T) {
		err := svc.OverrideField("override@x.com", "unknown")
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("err = %v, want 'unknown field'", err)
		}
	})

	t.Run("missing client returns error", func(t *testing.T) {
		err := svc.OverrideField("no-such@x.com", "totalGB")
		if err == nil || !strings.Contains(err.Error(), "record not found") {
			t.Fatalf("err = %v, want 'record not found'", err)
		}
	})
}

func TestReturnToTariff(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	profile := model.Profile{Name: "return-p", Traffic: int64Ptr(100), LimitIP: intPtr(10), ExpiryDays: intPtr(60)}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile: %v", err)
	}
	tariff := model.Tariff{Name: "return-t", TrafficStrategy: model.StrategyOverwrite}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatalf("create tariff: %v", err)
	}
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "return-group", TariffID: &tariff.Id}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	client := model.ClientRecord{
		Email:      "return@x.com",
		Group:      "return-group",
		TotalGB:    5,
		LimitIP:    1,
		ExpiryTime: 0,
	}
	db.Create(&client)
	totalGBOverride := int64(100)
	limitIPOverride := 10
	expiryOverride := int64(1702592000000)
	db.Create(&model.ClientTariff{
		ClientID:           client.Id,
		TariffID:           tariff.Id,
		StartedAt:          int64(1700000000000),
		TotalGBOverride:    &totalGBOverride,
		LimitIPOverride:    &limitIPOverride,
		ExpiryTimeOverride: &expiryOverride,
	})

	svc := &ClientTariffService{}

	t.Run("return totalGB clears CT override", func(t *testing.T) {
		if err := svc.ReturnToTariff("return@x.com", "totalGB"); err != nil {
			t.Fatalf("ReturnToTariff totalGB: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		if ct.TotalGBOverride != nil {
			t.Error("TotalGBOverride should be nil after return")
		}
	})

	t.Run("return limitIP clears CT override", func(t *testing.T) {
		if err := svc.ReturnToTariff("return@x.com", "limitIP"); err != nil {
			t.Fatalf("ReturnToTariff limitIP: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		if ct.LimitIPOverride != nil {
			t.Error("LimitIPOverride should be nil after return")
		}
	})

	t.Run("return expiryTime clears CT override", func(t *testing.T) {
		if err := svc.ReturnToTariff("return@x.com", "expiryTime"); err != nil {
			t.Fatalf("ReturnToTariff expiryTime: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		if ct.ExpiryTimeOverride != nil {
			t.Error("ExpiryTimeOverride should be nil after return")
		}
	})

	t.Run("unknown field returns error", func(t *testing.T) {
		err := svc.ReturnToTariff("return@x.com", "unknown")
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("err = %v, want 'unknown field'", err)
		}
	})

	t.Run("missing client returns error", func(t *testing.T) {
		err := svc.ReturnToTariff("no-such@x.com", "totalGB")
		if err == nil || !strings.Contains(err.Error(), "record not found") {
			t.Fatalf("err = %v, want 'record not found'", err)
		}
	})
}

func TestOverrideField_NonTariffClient(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	client := model.ClientRecord{
		Email:      "notariff@x.com",
		TotalGB:    10,
		ExpiryTime: 999,
	}
	db.Create(&client)

	svc := &ClientTariffService{}

	t.Run("override on client without active tariff returns error", func(t *testing.T) {
		err := svc.OverrideField("notariff@x.com", "totalGB")
		if !errors.Is(err, ErrNoActiveTariff) {
			t.Fatalf("err = %v, want ErrNoActiveTariff", err)
		}
	})
}

func TestActivateClientTariff(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	tariff := model.Tariff{Name: "reset-t"}
	db.Create(&tariff)

	client := model.ClientRecord{
		Email: "reset-flags@x.com",
		Group: "reset-group",
	}
	db.Create(&client)
	db.Create(&model.ClientGroup{Name: "reset-group", TariffID: &tariff.Id})

	t.Run("activateClientTariff creates ClientTariff and closes old", func(t *testing.T) {
		activateClientTariff(nil, client.Id, tariff.Id)

		var ct model.ClientTariff
		if err := db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct).Error; err != nil {
			t.Fatalf("active ClientTariff not created: %v", err)
		}
		if ct.TariffID != tariff.Id {
			t.Errorf("TariffID = %d, want %d", ct.TariffID, tariff.Id)
		}
		if ct.TotalGBOverride != nil || ct.LimitIPOverride != nil || ct.ExpiryTimeOverride != nil {
			t.Error("new CT row should have no overrides")
		}
		if ct.IsInboundsOverridden {
			t.Error("new CT row should have IsInboundsOverridden = false")
		}
	})

	t.Run("activateClientTariff ends previous active tariff", func(t *testing.T) {
		activateClientTariff(nil, client.Id, tariff.Id)

		var count int64
		db.Model(&model.ClientTariff{}).Where("client_id = ? AND ended_at IS NULL", client.Id).Count(&count)
		if count != 1 {
			t.Errorf("active rows = %d, want exactly 1", count)
		}

		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NOT NULL", client.Id).Order("ended_at DESC").First(&ct)
		if ct.EndedAt == nil {
			t.Error("previous tariff should have ended_at set")
		}
	})
}

func TestOverrideField_InboundsFlag(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profile := model.Profile{Name: "ib-ov-p", InboundIds: "[10,20]"}
	db.Create(&profile)
	tariff := model.Tariff{Name: "ib-ov-t", InboundStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "ib-ov-group", TariffID: &tariff.Id}
	db.Create(&group)

	client := model.ClientRecord{Email: "ib-ov@x.com", Group: "ib-ov-group"}
	db.Create(&client)
	db.Create(&model.ClientTariff{ClientID: client.Id, TariffID: tariff.Id, StartedAt: int64(1700000000000)})

	svc := &ClientTariffService{}

	t.Run("OverrideField inbounds sets flag on CT row", func(t *testing.T) {
		if err := svc.OverrideField("ib-ov@x.com", "inbounds"); err != nil {
			t.Fatalf("OverrideField inbounds: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		if !ct.IsInboundsOverridden {
			t.Error("IsInboundsOverridden should be true")
		}
	})

	t.Run("ReturnToTariff inbounds clears flag on CT row", func(t *testing.T) {
		if err := svc.ReturnToTariff("ib-ov@x.com", "inbounds"); err != nil {
			t.Fatalf("ReturnToTariff inbounds: %v", err)
		}
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", client.Id).First(&ct)
		if ct.IsInboundsOverridden {
			t.Error("IsInboundsOverridden should be false after return")
		}
	})
}
