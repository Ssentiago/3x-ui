package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func initProfileTestDB(t *testing.T) {
	t.Helper()
	_ = database.CloseDB()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestProfileService_Create(t *testing.T) {
	initProfileTestDB(t)
	svc := &ProfileService{}

	t.Run("success with all fields", func(t *testing.T) {
		summary, err := svc.Create("BASE", int64Ptr(100), intPtr(30), intPtr(3), []int{1, 2})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if summary.Name != "BASE" {
			t.Errorf("Name = %q, want BASE", summary.Name)
		}
		if summary.Traffic == nil || *summary.Traffic != 100 {
			t.Errorf("Traffic = %v, want 100", summary.Traffic)
		}
		if summary.ExpiryDays == nil || *summary.ExpiryDays != 30 {
			t.Errorf("ExpiryDays = %v, want 30", summary.ExpiryDays)
		}
		if summary.LimitIP == nil || *summary.LimitIP != 3 {
			t.Errorf("LimitIP = %v, want 3", summary.LimitIP)
		}
		if len(summary.InboundIds) != 2 || summary.InboundIds[0] != 1 || summary.InboundIds[1] != 2 {
			t.Errorf("InboundIds = %v, want [1,2]", summary.InboundIds)
		}
		if summary.Id == 0 {
			t.Error("Id should not be zero")
		}
	})

	t.Run("success with nullable fields nil", func(t *testing.T) {
		summary, err := svc.Create("MINIMAL", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if summary.Traffic != nil {
			t.Errorf("Traffic = %v, want nil", summary.Traffic)
		}
		if summary.ExpiryDays != nil {
			t.Errorf("ExpiryDays = %v, want nil", summary.ExpiryDays)
		}
		if summary.LimitIP != nil {
			t.Errorf("LimitIP = %v, want nil", summary.LimitIP)
		}
	})

	t.Run("empty name returns error", func(t *testing.T) {
		_, err := svc.Create("", int64Ptr(10), nil, nil, nil)
		if err == nil || err.Error() != "profile name is required" {
			t.Fatalf("err = %v, want 'profile name is required'", err)
		}
	})

	t.Run("negative traffic returns error", func(t *testing.T) {
		_, err := svc.Create("Neg", int64Ptr(-1), nil, nil, nil)
		if err == nil || err.Error() != "traffic must be non-negative" {
			t.Fatalf("err = %v, want 'traffic must be non-negative'", err)
		}
	})

	t.Run("negative expiryDays returns error", func(t *testing.T) {
		_, err := svc.Create("Neg", nil, intPtr(-1), nil, nil)
		if err == nil || err.Error() != "expiryDays must be non-negative" {
			t.Fatalf("err = %v, want 'expiryDays must be non-negative'", err)
		}
	})

	t.Run("negative limitIP returns error", func(t *testing.T) {
		_, err := svc.Create("Neg", nil, nil, intPtr(-1), nil)
		if err == nil || err.Error() != "limitIP must be non-negative" {
			t.Fatalf("err = %v, want 'limitIP must be non-negative'", err)
		}
	})
}

func TestProfileService_Update(t *testing.T) {
	initProfileTestDB(t)
	svc := &ProfileService{}

	summary, err := svc.Create("Original", int64Ptr(10), intPtr(7), intPtr(1), []int{1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("update all fields", func(t *testing.T) {
		updated, err := svc.Update(summary.Id, "Updated", int64Ptr(200), intPtr(90), intPtr(5), []int{3, 4})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.Name != "Updated" {
			t.Errorf("Name = %q, want Updated", updated.Name)
		}
		if updated.Traffic == nil || *updated.Traffic != 200 {
			t.Errorf("Traffic = %v, want 200", updated.Traffic)
		}
		if updated.ExpiryDays == nil || *updated.ExpiryDays != 90 {
			t.Errorf("ExpiryDays = %v, want 90", updated.ExpiryDays)
		}
		if updated.LimitIP == nil || *updated.LimitIP != 5 {
			t.Errorf("LimitIP = %v, want 5", updated.LimitIP)
		}
		if len(updated.InboundIds) != 2 {
			t.Errorf("InboundIds count = %d, want 2", len(updated.InboundIds))
		}
	})

	t.Run("partial update keeps existing inboundIds when nil", func(t *testing.T) {
		updated, err := svc.Update(summary.Id, "Partial", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		// nil inboundIds should keep existing [3,4] from previous update
		if len(updated.InboundIds) != 2 {
			t.Errorf("InboundIds count = %d, want 2 (kept from previous)", len(updated.InboundIds))
		}
		// nil traffic should overwrite to nil
		if updated.Traffic != nil {
			t.Errorf("Traffic = %v, want nil", updated.Traffic)
		}
	})

	t.Run("update nil-ifies previously set fields", func(t *testing.T) {
		updated, err := svc.Update(summary.Id, "Nilled", nil, nil, nil, []int{})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.Traffic != nil {
			t.Errorf("Traffic = %v, want nil", updated.Traffic)
		}
		if len(updated.InboundIds) != 0 {
			t.Errorf("InboundIds count = %d, want 0", len(updated.InboundIds))
		}
	})

	t.Run("non-existent id returns error", func(t *testing.T) {
		_, err := svc.Update(99999, "X", nil, nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "record not found") {
			t.Fatalf("err = %v, want 'record not found'", err)
		}
	})

	t.Run("empty name returns error", func(t *testing.T) {
		_, err := svc.Update(summary.Id, "", nil, nil, nil, nil)
		if err == nil || err.Error() != "profile name is required" {
			t.Fatalf("err = %v, want 'profile name is required'", err)
		}
	})
}

func TestProfileService_Delete(t *testing.T) {
	initProfileTestDB(t)
	svc := &ProfileService{}

	t.Run("delete unused profile succeeds", func(t *testing.T) {
		summary, err := svc.Create("DelMe", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := svc.Delete(summary.Id); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		_, err = svc.Get(summary.Id)
		if err == nil || !strings.Contains(err.Error(), "record not found") {
			t.Fatalf("Get after delete: err = %v, want 'record not found'", err)
		}
	})

	t.Run("delete profile used by tariff fails with tariff names", func(t *testing.T) {
		summary, err := svc.Create("Used", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		db := database.GetDB()
		tariff := model.Tariff{Name: "GoldTariff"}
		db.Create(&tariff)
		db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: summary.Id, Position: 0})

		err = svc.Delete(summary.Id)
		if err == nil || !strings.Contains(err.Error(), "profile is used by tariffs") {
			t.Fatalf("err = %v, want 'profile is used by tariffs'", err)
		}
	})
}

func TestProfileService_List(t *testing.T) {
	initProfileTestDB(t)
	svc := &ProfileService{}

	t.Run("empty list", func(t *testing.T) {
		list, err := svc.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("len = %d, want 0", len(list))
		}
	})

	t.Run("list returns profiles with tariffCount", func(t *testing.T) {
		p1, _ := svc.Create("P1", nil, nil, nil, nil)
		p2, _ := svc.Create("P2", nil, nil, nil, nil)

		db := database.GetDB()
		tariff := model.Tariff{Name: "T"}
		db.Create(&tariff)
		db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p1.Id, Position: 0})

		list, err := svc.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("len = %d, want 2", len(list))
		}
		for _, p := range list {
			switch p.Id {
			case p1.Id:
				if p.TariffCount != 1 {
					t.Errorf("P1 TariffCount = %d, want 1", p.TariffCount)
				}
			case p2.Id:
				if p.TariffCount != 0 {
					t.Errorf("P2 TariffCount = %d, want 0", p.TariffCount)
				}
			}
		}
	})
}

func TestProfileService_Get(t *testing.T) {
	initProfileTestDB(t)
	svc := &ProfileService{}

	summary, err := svc.Create("BASE", int64Ptr(50), intPtr(30), intPtr(2), []int{1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("Get returns full summary", func(t *testing.T) {
		got, err := svc.Get(summary.Id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != "BASE" {
			t.Errorf("Name = %q, want BASE", got.Name)
		}
		if got.Traffic == nil || *got.Traffic != 50 {
			t.Errorf("Traffic = %v, want 50", got.Traffic)
		}
		if got.ExpiryDays == nil || *got.ExpiryDays != 30 {
			t.Errorf("ExpiryDays = %v, want 30", got.ExpiryDays)
		}
		if got.LimitIP == nil || *got.LimitIP != 2 {
			t.Errorf("LimitIP = %v, want 2", got.LimitIP)
		}
		if len(got.InboundIds) != 1 || got.InboundIds[0] != 1 {
			t.Errorf("InboundIds = %v, want [1]", got.InboundIds)
		}
	})

	t.Run("Get non-existent id returns error", func(t *testing.T) {
		_, err := svc.Get(99999)
		if err == nil || !strings.Contains(err.Error(), "record not found") {
			t.Fatalf("err = %v, want 'record not found'", err)
		}
	})
}

func TestProfileService_CreateDuplicateName(t *testing.T) {
	initProfileTestDB(t)
	svc := &ProfileService{}

	_, err := svc.Create("UNIQUE", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err = svc.Create("UNIQUE", nil, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("err = %v, want UNIQUE constraint error", err)
	}
}
