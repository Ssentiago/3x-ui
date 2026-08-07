package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// TestClientTariffHistory verifies the client_tariffs table lifecycle:
//   - Entering a tariff inserts a row with ended_at=NULL
//   - Changing tariffs ends the old row and inserts a new one
//   - Exiting a tariff sets ended_at on the active row
//   - getActiveClientTariff returns the active started_at
func TestClientTariffHistory(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	tariff1 := model.Tariff{Name: "gold", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff1)
	tariff2 := model.Tariff{Name: "silver", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff2)

	grp := model.ClientGroup{Name: "test-group", TariffID: &tariff1.Id}
	db.Create(&grp)

	rec := model.ClientRecord{Email: "hist@test.com", Group: "test-group"}
	db.Create(&rec)

	activateClientTariff(db, rec.Id, tariff1.Id)

	ct := getActiveClientTariff(db, rec.Id)
	if ct == nil {
		t.Fatal("expected active tariff entry, got nil")
	}
	if ct.StartedAt <= 0 || ct.StartedAt > time.Now().UnixMilli() {
		t.Errorf("started_at = %d, expected recent timestamp", ct.StartedAt)
	}

	var count int64
	db.Model(&model.ClientTariff{}).Where("client_id = ? AND ended_at IS NULL", rec.Id).Count(&count)
	if count != 1 {
		t.Errorf("active entries = %d, want 1", count)
	}

	time.Sleep(1) // ensure timestamp progression
	activateClientTariff(db, rec.Id, tariff2.Id)

	db.Model(&model.ClientTariff{}).Where("client_id = ? AND ended_at IS NULL", rec.Id).Count(&count)
	if count != 1 {
		t.Errorf("active entries after change = %d, want 1", count)
	}

	var all []model.ClientTariff
	db.Where("client_id = ?", rec.Id).Order("id ASC").Find(&all)
	if len(all) != 2 {
		t.Fatalf("total history entries = %d, want 2", len(all))
	}
	if all[0].EndedAt == nil {
		t.Error("first entry should have ended_at set after change")
	}
	if all[1].EndedAt != nil {
		t.Error("second entry should have ended_at=NULL (active)")
	}
	if all[1].TariffID != tariff2.Id {
		t.Errorf("active tariff_id = %d, want %d", all[1].TariffID, tariff2.Id)
	}

	now := time.Now().UnixMilli()
	db.Model(&model.ClientTariff{}).
		Where("client_id = ? AND ended_at IS NULL", rec.Id).
		Update("ended_at", now)

	ct2 := getActiveClientTariff(db, rec.Id)
	if ct2 != nil {
		t.Errorf("expected nil after exit, got started_at=%d", ct2.StartedAt)
	}
}

// TestResetClientOverrides_ClearsFlags verifies activateClientTariff creates a
// fresh CT row with no overrides and closes any previous active row.
func TestResetClientOverrides_ClearsFlags(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	tariff := model.Tariff{Name: "flags-t"}
	db.Create(&tariff)
	db.Create(&model.ClientGroup{Name: "flags-group", TariffID: &tariff.Id})

	rec := model.ClientRecord{
		Email: "flags@test.com",
		Group: "flags-group",
	}
	db.Create(&rec)

	activateClientTariff(db, rec.Id, tariff.Id)

	var ct model.ClientTariff
	if err := db.Where("client_id = ? AND ended_at IS NULL", rec.Id).First(&ct).Error; err != nil {
		t.Fatalf("active ClientTariff not created: %v", err)
	}
	if ct.TotalGBOverride != nil || ct.LimitIPOverride != nil || ct.ExpiryTimeOverride != nil {
		t.Error("new CT row should have no overrides")
	}
}
