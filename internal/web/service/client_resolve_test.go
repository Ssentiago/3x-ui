package service

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestResolveForGroup(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	profile := model.Profile{Name: "rfg-p", Traffic: int64Ptr(80), LimitIP: intPtr(4), ExpiryDays: intPtr(45)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "rfg-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	tariffGroup := model.ClientGroup{Name: "rfg-tariff-group", TariffID: &tariff.Id}
	noTariffGroup := model.ClientGroup{Name: "rfg-free-group"}
	db.Create(&tariffGroup)
	db.Create(&noTariffGroup)

	svc := &ClientService{}

	t.Run("returns resolved values for tariff group with active ClientTariff", func(t *testing.T) {
		client := model.ClientRecord{
			Email:      "rfg-active@x.com",
			Group:      "original-group",
			TotalGB:    10,
			LimitIP:    1,
			ExpiryTime: 999,
		}
		db.Create(&client)
		startedAt := int64(1700000000000)
		db.Create(&model.ClientTariff{ClientID: client.Id, TariffID: tariff.Id, StartedAt: startedAt})

		result, err := svc.ResolveForGroup("rfg-active@x.com", "rfg-tariff-group")
		if err != nil {
			t.Fatalf("ResolveForGroup: %v", err)
		}
		wantTotal := int64(80) * bytesPerGB
		if result.TotalGB != wantTotal {
			t.Errorf("TotalGB = %d, want %d", result.TotalGB, wantTotal)
		}
		wantExpiry := startedAt + 45*86400*1000
		if result.ExpiryTime != wantExpiry {
			t.Errorf("ExpiryTime = %d, want %d", result.ExpiryTime, wantExpiry)
		}
		if result.LimitIP != 4 {
			t.Errorf("LimitIP = %d, want 4", result.LimitIP)
		}

		// Client's original group should be restored
		var rec model.ClientRecord
		db.First(&rec, client.Id)
		if rec.Group != "original-group" {
			t.Errorf("client group was not restored: got %q, want original-group", rec.Group)
		}
	})

	t.Run("delayed start returns negative expiry offset", func(t *testing.T) {
		client := model.ClientRecord{
			Email:      "rfg-delayed@x.com",
			Group:      "original-group",
			TotalGB:    10,
			ExpiryTime: 0,
		}
		db.Create(&client)

		result, err := svc.ResolveForGroup("rfg-delayed@x.com", "rfg-tariff-group")
		if err != nil {
			t.Fatalf("ResolveForGroup: %v", err)
		}
		// No active ClientTariff → ExpiryTime = -TariffExpiryDays * 86400 * 1000
		wantExpiry := -int64(45) * 86400 * 1000
		if result.ExpiryTime != wantExpiry {
			t.Errorf("ExpiryTime = %d, want %d (negative delayed-start offset)", result.ExpiryTime, wantExpiry)
		}
	})

	t.Run("non-tariff group returns raw client values", func(t *testing.T) {
		client := model.ClientRecord{
			Email:      "rfg-free@x.com",
			Group:      "original-group",
			TotalGB:    50,
			LimitIP:    3,
			ExpiryTime: 1700000000000,
		}
		db.Create(&client)

		result, err := svc.ResolveForGroup("rfg-free@x.com", "rfg-free-group")
		if err != nil {
			t.Fatalf("ResolveForGroup: %v", err)
		}
		if result.TotalGB != 50 {
			t.Errorf("TotalGB = %d, want 50 (raw)", result.TotalGB)
		}
		if result.ExpiryTime != 1700000000000 {
			t.Errorf("ExpiryTime = %d, want 1700000000000 (raw)", result.ExpiryTime)
		}
		if result.LimitIP != 3 {
			t.Errorf("LimitIP = %d, want 3 (raw)", result.LimitIP)
		}
	})

	t.Run("overridden fields keep client value in preview", func(t *testing.T) {
		client := model.ClientRecord{
			Email:                  "rfg-override@x.com",
			Group:                  "original-group",
			TotalGB:                99,
			ExpiryTime:             888,
		}
		db.Create(&client)
		gbOverride := int64(99)
		expOverride := int64(888)
		db.Create(&model.ClientTariff{
			ClientID: client.Id, TariffID: tariff.Id, StartedAt: int64(1700000000000),
			TotalGBOverride: &gbOverride, ExpiryTimeOverride: &expOverride,
		})

		result, err := svc.ResolveForGroup("rfg-override@x.com", "rfg-tariff-group")
		if err != nil {
			t.Fatalf("ResolveForGroup: %v", err)
		}
		if result.TotalGB != 99 {
			t.Errorf("TotalGB = %d, want 99 (overridden)", result.TotalGB)
		}
		if result.ExpiryTime != 888 {
			t.Errorf("ExpiryTime = %d, want 888 (overridden, not delayed)", result.ExpiryTime)
		}
	})

	t.Run("non-existent email returns error", func(t *testing.T) {
		_, err := svc.ResolveForGroup("no-such@x.com", "any-group")
		if err == nil {
			t.Fatal("expected error for non-existent email")
		}
	})
}

func TestGetActiveClientTariffMap(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()
	tariff := model.Tariff{Name: "sat-t"}
	db.Create(&tariff)
	group := model.ClientGroup{Name: "sat-group", TariffID: &tariff.Id}
	db.Create(&group)

	started1 := int64(1700000000000)
	started2 := int64(1800000000000)

	client1 := model.ClientRecord{Email: "sat1@x.com", Group: "sat-group"}
	client2 := model.ClientRecord{Email: "sat2@x.com", Group: "sat-group"}
	client3 := model.ClientRecord{Email: "sat3@x.com", Group: "sat-group"} // no active tariff
	db.Create(&client1)
	db.Create(&client2)
	db.Create(&client3)
	db.Create(&model.ClientTariff{ClientID: client1.Id, TariffID: tariff.Id, StartedAt: started1})
	db.Create(&model.ClientTariff{ClientID: client2.Id, TariffID: tariff.Id, StartedAt: started2})
	// client2 also has an ended entry
	db.Create(&model.ClientTariff{ClientID: client2.Id, TariffID: tariff.Id, StartedAt: started2 - 1000, EndedAt: int64Ptr(started2)})

	t.Run("returns started_at for active clients, absent for inactive", func(t *testing.T) {
		result := GetActiveClientTariffMap(db, []int{client1.Id, client2.Id, client3.Id, 99999})

		if v, ok := result[client1.Id]; !ok || v == nil || v.StartedAt != started1 {
			t.Errorf("client1: got %v, want %d", v, started1)
		}
		if v, ok := result[client2.Id]; !ok || v == nil || v.StartedAt != started2 {
			t.Errorf("client2: got %v, want %d (active only)", v, started2)
		}
		if _, ok := result[client3.Id]; ok {
			t.Error("client3 should be absent (no active ClientTariff)")
		}
		if _, ok := result[99999]; ok {
			t.Error("non-existent client should be absent")
		}
	})

	t.Run("empty input returns empty map", func(t *testing.T) {
		result := GetActiveClientTariffMap(db, nil)
		if len(result) != 0 {
			t.Errorf("len = %d, want 0", len(result))
		}
	})
}

func TestClientBatchResolver(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profile1 := model.Profile{Name: "cbr-p1", Traffic: int64Ptr(50), LimitIP: intPtr(3), ExpiryDays: intPtr(30)}
	profile2 := model.Profile{Name: "cbr-p2", Traffic: int64Ptr(20)}
	db.Create(&profile1)
	db.Create(&profile2)

	tariff1 := model.Tariff{Name: "cbr-t1", TrafficStrategy: model.StrategySum}
	tariff2 := model.Tariff{Name: "cbr-t2", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff1)
	db.Create(&tariff2)
	db.Create(&model.TariffProfile{TariffID: tariff1.Id, ProfileID: profile1.Id, Position: 0})
	db.Create(&model.TariffProfile{TariffID: tariff1.Id, ProfileID: profile2.Id, Position: 1})
	db.Create(&model.TariffProfile{TariffID: tariff2.Id, ProfileID: profile1.Id, Position: 0})

	group1 := model.ClientGroup{Name: "cbr-group1", TariffID: &tariff1.Id}
	group2 := model.ClientGroup{Name: "cbr-group2", TariffID: &tariff2.Id}
	freeGroup := model.ClientGroup{Name: "cbr-free"}
	db.Create(&group1)
	db.Create(&group2)
	db.Create(&freeGroup)

	startedAt := int64(1700000000000)

	client1 := model.ClientRecord{Email: "cbr1@x.com", Group: "cbr-group1", TotalGB: 5, LimitIP: 1, ExpiryTime: 1}
	client2 := model.ClientRecord{Email: "cbr2@x.com", Group: "cbr-group2", TotalGB: 5, LimitIP: 1, ExpiryTime: 1}
	client3 := model.ClientRecord{Email: "cbr3@x.com", Group: "cbr-free", TotalGB: 30, LimitIP: 2, ExpiryTime: 1}
	client4 := model.ClientRecord{Email: "cbr4@x.com", TotalGB: 99, LimitIP: 9, ExpiryTime: 1} // no group
	client5 := model.ClientRecord{
		Email:                 "cbr5@x.com",
		Group:                 "cbr-group2",
		TotalGB:               7,
		ExpiryTime:            999,
	}
	db.Create(&client1)
	db.Create(&client2)
	db.Create(&client3)
	db.Create(&client4)
	db.Create(&client5)
	db.Create(&model.ClientTariff{ClientID: client1.Id, TariffID: tariff1.Id, StartedAt: startedAt})
	db.Create(&model.ClientTariff{ClientID: client2.Id, TariffID: tariff2.Id, StartedAt: startedAt})
	gbOverride5 := int64(7)
	db.Create(&model.ClientTariff{
		ClientID: client5.Id, TariffID: tariff2.Id, StartedAt: startedAt,
		TotalGBOverride: &gbOverride5,
	})

	t.Run("batch resolver matches single resolver for limits", func(t *testing.T) {
		byId := map[int]*model.ClientRecord{
			client1.Id: &client1,
			client2.Id: &client2,
			client3.Id: &client3,
			client4.Id: &client4,
			client5.Id: &client5,
		}
		resolver := NewBatchResolver(db, byId)

		for id, rec := range byId {
			single := ResolveClientLimits(db, rec)
			batch := resolver.ResolveLimits(rec)

			if single.TotalGB != batch.TotalGB {
				t.Errorf("client %d TotalGB: single=%d batch=%d", id, single.TotalGB, batch.TotalGB)
			}
			if single.ExpiryTime != batch.ExpiryTime {
				t.Errorf("client %d ExpiryTime: single=%d batch=%d", id, single.ExpiryTime, batch.ExpiryTime)
			}
			if single.LimitIP != batch.LimitIP {
				t.Errorf("client %d LimitIP: single=%d batch=%d", id, single.LimitIP, batch.LimitIP)
			}
		}
	})

	t.Run("sum tariff correctly aggregates traffic", func(t *testing.T) {
		byId := map[int]*model.ClientRecord{client1.Id: &client1}
		resolver := NewBatchResolver(db, byId)
		result := resolver.ResolveLimits(&client1)
		wantTotal := int64(50+20) * bytesPerGB
		if result.TotalGB != wantTotal {
			t.Errorf("TotalGB = %d, want %d (sum of 50+20)", result.TotalGB, wantTotal)
		}
	})

	t.Run("nil resolver is safe", func(t *testing.T) {
		var resolver *ClientBatchResolver
		result := resolver.ResolveLimits(&client3)
		if result.TotalGB != 30 {
			t.Errorf("TotalGB = %d, want 30 (raw)", result.TotalGB)
		}
	})

	t.Run("empty groups creates resolver with no configs", func(t *testing.T) {
		byId := map[int]*model.ClientRecord{client4.Id: &client4}
		resolver := NewBatchResolver(db, byId)
		result := resolver.ResolveLimits(&client4)
		if result.TotalGB != client4.TotalGB {
			t.Errorf("TotalGB = %d, want %d (no tariff group)", result.TotalGB, client4.TotalGB)
		}
	})

	t.Run("overridden fields bypass resolution", func(t *testing.T) {
		byId := map[int]*model.ClientRecord{client5.Id: &client5}
		resolver := NewBatchResolver(db, byId)
		result := resolver.ResolveLimits(&client5)
		if result.TotalGB != 7 {
			t.Errorf("TotalGB = %d, want 7 (overridden)", result.TotalGB)
		}
	})
}

func TestResolveClientFields_InboundStrategy(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	profile := model.Profile{Name: "ib-p", InboundIds: "[10,20]"}
	db.Create(&profile)

	unionTariff := model.Tariff{Name: "ib-union", InboundStrategy: model.StrategyUnion}
	overwriteTariff := model.Tariff{Name: "ib-overwrite", InboundStrategy: model.StrategyOverwrite}
	db.Create(&unionTariff)
	db.Create(&overwriteTariff)
	db.Create(&model.TariffProfile{TariffID: unionTariff.Id, ProfileID: profile.Id, Position: 0})
	db.Create(&model.TariffProfile{TariffID: overwriteTariff.Id, ProfileID: profile.Id, Position: 10})

	unionGroup := model.ClientGroup{Name: "ib-union-group", TariffID: &unionTariff.Id}
	owGroup := model.ClientGroup{Name: "ib-ow-group", TariffID: &overwriteTariff.Id}
	db.Create(&unionGroup)
	db.Create(&owGroup)

	// Union client with own inbound 5
	unionClient := model.ClientRecord{Email: "ib-union@x.com", Group: "ib-union-group"}
	db.Create(&unionClient)
	db.Create(&model.ClientInbound{ClientId: unionClient.Id, InboundId: 5})

	// Overwrite client with own inbound 7
	owClient := model.ClientRecord{Email: "ib-ow@x.com", Group: "ib-ow-group"}
	db.Create(&owClient)
	db.Create(&model.ClientInbound{ClientId: owClient.Id, InboundId: 7})

	t.Run("union merges own and chain inbounds", func(t *testing.T) {
		result := ResolveClientFields(nil, nil, &unionClient)
		expected := []int{5, 10, 20}
		sort.Ints(result.InboundIds)
		if !intSlicesEqual(result.InboundIds, expected) {
			t.Errorf("InboundIds = %v, want %v (union of own [5] + chain [10,20])", result.InboundIds, expected)
		}
	})

	t.Run("overwrite replaces own inbounds with chain", func(t *testing.T) {
		result := ResolveClientFields(nil, nil, &owClient)
		expected := []int{10, 20}
		if !intSlicesEqual(result.InboundIds, expected) {
			t.Errorf("InboundIds = %v, want %v (overwrite)", result.InboundIds, expected)
		}
	})

	t.Run("overridden inbounds keeps own", func(t *testing.T) {
		db.Create(&model.ClientTariff{
			ClientID: unionClient.Id, TariffID: unionTariff.Id, StartedAt: int64(1700000000000),
			IsInboundsOverridden: true,
		})
		result := ResolveClientFields(nil, nil, &unionClient)
		expected := []int{5}
		if !intSlicesEqual(result.InboundIds, expected) {
			t.Errorf("InboundIds = %v, want %v (overridden keeps own)", result.InboundIds, expected)
		}
	})
}

func intSlicesEqual(a, b []int) bool {
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
