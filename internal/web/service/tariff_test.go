package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// --- parseInboundIds / marshalInboundIds (pure unit) ---

func TestParseInboundIds(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []int
		wantErr bool
	}{
		{name: "empty string", raw: "", want: []int{}, wantErr: false},
		{name: "null literal", raw: "null", want: []int{}, wantErr: false},
		{name: "valid single", raw: "[1]", want: []int{1}, wantErr: false},
		{name: "valid multiple", raw: "[1,2,3]", want: []int{1, 2, 3}, wantErr: false},
		{name: "malformed", raw: "not-json", want: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInboundIds(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && !sliceEq(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarshalInboundIds(t *testing.T) {
	tests := []struct {
		name    string
		ids     []int
		want    string
		wantErr bool
	}{
		{name: "nil", ids: nil, want: "", wantErr: false},
		{name: "empty", ids: []int{}, want: "[]", wantErr: false},
		{name: "single", ids: []int{1}, want: "[1]", wantErr: false},
		{name: "multiple", ids: []int{1, 2, 3}, want: "[1,2,3]", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalInboundIds(tt.ids)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func sliceEq(a, b []int) bool {
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

// --- TariffService CRUD ---

func initTariffTestDB(t *testing.T) {
	t.Helper()
	_ = database.CloseDB()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestTariffService_Create(t *testing.T) {
	initTariffTestDB(t)
	svc := &TariffService{}

	t.Run("success with default strategies", func(t *testing.T) {
		summary, err := svc.Create("Gold", TariffStrategies{})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if summary.Name != "Gold" {
			t.Errorf("Name = %q, want Gold", summary.Name)
		}
		if summary.TrafficStrategy != model.StrategyOverwrite {
			t.Errorf("TrafficStrategy = %q, want overwrite", summary.TrafficStrategy)
		}
		if summary.InboundStrategy != model.StrategyOverwrite {
			t.Errorf("InboundStrategy = %q, want overwrite", summary.InboundStrategy)
		}
		if summary.Id == 0 {
			t.Error("Id should not be zero")
		}
		if summary.Resolved == nil {
			t.Error("Resolved should not be nil")
		}
	})

	t.Run("empty name returns error", func(t *testing.T) {
		_, err := svc.Create("", TariffStrategies{})
		if err == nil || err.Error() != "tariff name is required" {
			t.Fatalf("err = %v, want 'tariff name is required'", err)
		}
	})

	t.Run("explicit strategies are preserved", func(t *testing.T) {
		summary, err := svc.Create("SumTariff", TariffStrategies{
			TrafficStrategy: model.StrategySum,
			InboundStrategy: model.StrategyUnion,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if summary.TrafficStrategy != model.StrategySum {
			t.Errorf("TrafficStrategy = %q, want sum", summary.TrafficStrategy)
		}
		if summary.InboundStrategy != model.StrategyUnion {
			t.Errorf("InboundStrategy = %q, want union", summary.InboundStrategy)
		}
	})
}

func TestTariffService_Update(t *testing.T) {
	initTariffTestDB(t)
	svc := &TariffService{}

	summary, err := svc.Create("Gold", TariffStrategies{TrafficStrategy: model.StrategyOverwrite})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("update name and strategy", func(t *testing.T) {
		updated, err := svc.Update(summary.Id, "Platinum", TariffStrategies{
			TrafficStrategy: model.StrategySum,
			InboundStrategy: model.StrategyUnion,
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.Name != "Platinum" {
			t.Errorf("Name = %q, want Platinum", updated.Name)
		}
		if updated.TrafficStrategy != model.StrategySum {
			t.Errorf("TrafficStrategy = %q, want sum", updated.TrafficStrategy)
		}
		if updated.InboundStrategy != model.StrategyUnion {
			t.Errorf("InboundStrategy = %q, want union", updated.InboundStrategy)
		}
		if updated.Id != summary.Id {
			t.Errorf("Id changed from %d to %d", summary.Id, updated.Id)
		}
	})

	t.Run("non-existent id returns error", func(t *testing.T) {
		_, err := svc.Update(99999, "X", TariffStrategies{})
		if err == nil || !strings.Contains(err.Error(), "record not found") {
			t.Fatalf("err = %v, want 'record not found'", err)
		}
	})

	t.Run("empty name returns error", func(t *testing.T) {
		_, err := svc.Update(summary.Id, "", TariffStrategies{})
		if err == nil || err.Error() != "tariff name is required" {
			t.Fatalf("err = %v, want 'tariff name is required'", err)
		}
	})
}

func TestTariffService_Delete(t *testing.T) {
	initTariffTestDB(t)
	svc := &TariffService{}

	t.Run("delete unused tariff succeeds", func(t *testing.T) {
		summary, err := svc.Create("Gold", TariffStrategies{})
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

	t.Run("delete tariff used by group fails", func(t *testing.T) {
		summary, err := svc.Create("Silver", TariffStrategies{})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		db := database.GetDB()
		if err := db.Create(&model.ClientGroup{Name: "vip", TariffID: &summary.Id}).Error; err != nil {
			t.Fatalf("create group: %v", err)
		}
		err = svc.Delete(summary.Id)
		if err == nil || !strings.Contains(err.Error(), "tariff is used by") {
			t.Fatalf("err = %v, want 'tariff is used by'", err)
		}
	})
}

func TestTariffService_SetProfiles(t *testing.T) {
	initTariffTestDB(t)
	svc := &TariffService{}

	db := database.GetDB()
	p1 := model.Profile{Name: "p1", Traffic: int64Ptr(10)}
	p2 := model.Profile{Name: "p2", Traffic: int64Ptr(20)}
	db.Create(&p1)
	db.Create(&p2)

	summary, err := svc.Create("T", TariffStrategies{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("success with contiguous positions", func(t *testing.T) {
		err := svc.SetProfiles(summary.Id, []ProfilePosition{
			{Id: p1.Id, Position: 0},
			{Id: p2.Id, Position: 1},
		})
		if err != nil {
			t.Fatalf("SetProfiles: %v", err)
		}
		got, err := svc.Get(summary.Id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if len(got.Profiles) != 2 {
			t.Fatalf("profiles count = %d, want 2", len(got.Profiles))
		}
		if got.Profiles[0].Id != p1.Id || got.Profiles[0].Position != 0 {
			t.Errorf("profile[0] = %+v, want id=%d pos=0", got.Profiles[0], p1.Id)
		}
		if got.Profiles[1].Id != p2.Id || got.Profiles[1].Position != 1 {
			t.Errorf("profile[1] = %+v, want id=%d pos=1", got.Profiles[1], p2.Id)
		}
	})

	t.Run("non-contiguous positions fail", func(t *testing.T) {
		err := svc.SetProfiles(summary.Id, []ProfilePosition{
			{Id: p1.Id, Position: 0},
			{Id: p2.Id, Position: 2},
		})
		if err == nil || !strings.Contains(err.Error(), "profile positions must be contiguous") {
			t.Fatalf("err = %v, want 'profile positions must be contiguous'", err)
		}
	})

	t.Run("replacing profiles works", func(t *testing.T) {
		err := svc.SetProfiles(summary.Id, []ProfilePosition{
			{Id: p2.Id, Position: 0},
		})
		if err != nil {
			t.Fatalf("SetProfiles replace: %v", err)
		}
		got, err := svc.Get(summary.Id)
		if err != nil {
			t.Fatalf("Get after replace: %v", err)
		}
		if len(got.Profiles) != 1 {
			t.Fatalf("profiles count = %d, want 1", len(got.Profiles))
		}
		if got.Profiles[0].Id != p2.Id {
			t.Errorf("profile[0].Id = %d, want %d", got.Profiles[0].Id, p2.Id)
		}
	})
}

func TestTariffService_List(t *testing.T) {
	initTariffTestDB(t)
	svc := &TariffService{}

	t.Run("empty list", func(t *testing.T) {
		list, err := svc.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("len = %d, want 0", len(list))
		}
	})

	t.Run("list returns tariffs ordered by id", func(t *testing.T) {
		svc.Create("B", TariffStrategies{})
		svc.Create("A", TariffStrategies{})
		list, err := svc.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("len = %d, want 2", len(list))
		}
		if list[0].Name != "B" {
			t.Errorf("first = %q, want B (ordered by id)", list[0].Name)
		}
	})

	t.Run("list includes groupCount", func(t *testing.T) {
		db := database.GetDB()
		summary, _ := svc.Create("WithGroup", TariffStrategies{})
		db.Create(&model.ClientGroup{Name: "g1", TariffID: &summary.Id})
		db.Create(&model.ClientGroup{Name: "g2", TariffID: &summary.Id})

		list, err := svc.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		var found *TariffSummary
		for i := range list {
			if list[i].Id == summary.Id {
				found = &list[i]
				break
			}
		}
		if found == nil {
			t.Fatal("tariff not found in list")
		}
		if found.GroupCount != 2 {
			t.Errorf("GroupCount = %d, want 2", found.GroupCount)
		}
	})
}

func TestTariffService_Get(t *testing.T) {
	initTariffTestDB(t)
	svc := &TariffService{}

	db := database.GetDB()
	p := model.Profile{Name: "base", Traffic: int64Ptr(50), ExpiryDays: intPtr(30)}
	db.Create(&p)

	summary, err := svc.Create("Gold", TariffStrategies{TrafficStrategy: model.StrategyOverwrite})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc.SetProfiles(summary.Id, []ProfilePosition{{Id: p.Id, Position: 0}})

	db.Create(&model.ClientGroup{Name: "vip", TariffID: &summary.Id})
	db.Create(&model.ClientRecord{Email: "a@x.com", Enable: true, Group: "vip", TotalGB: 0})

	t.Run("Get returns full summary", func(t *testing.T) {
		got, err := svc.Get(summary.Id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Name != "Gold" {
			t.Errorf("Name = %q, want Gold", got.Name)
		}
		if len(got.Profiles) != 1 {
			t.Errorf("Profiles count = %d, want 1", len(got.Profiles))
		}
		if got.Profiles[0].Name != "base" {
			t.Errorf("Profile name = %q, want base", got.Profiles[0].Name)
		}
		if got.Resolved == nil {
			t.Fatal("Resolved is nil")
		}
		if got.Resolved.Traffic != 50*bytesPerGB {
			t.Errorf("Resolved.Traffic = %d, want %d", got.Resolved.Traffic, 50*bytesPerGB)
		}
		if got.Resolved.ExpiryDays != 30 {
			t.Errorf("Resolved.ExpiryDays = %d, want 30", got.Resolved.ExpiryDays)
		}
		if got.GroupCount != 1 {
			t.Errorf("GroupCount = %d, want 1", got.GroupCount)
		}
		if got.ClientCount != 1 {
			t.Errorf("ClientCount = %d, want 1", got.ClientCount)
		}
	})

	t.Run("Get non-existent id returns error", func(t *testing.T) {
		_, err := svc.Get(99999)
		if err == nil || !strings.Contains(err.Error(), "record not found") {
			t.Fatalf("err = %v, want 'record not found'", err)
		}
	})
}

// --- rewriteTrafficForClients ---

func TestRewriteTrafficForClients(t *testing.T) {
	initTariffTestDB(t)
	db := database.GetDB()

	profile := model.Profile{Name: "traffic-profile", Traffic: int64Ptr(100), ExpiryDays: intPtr(60)}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile: %v", err)
	}
	tariff := model.Tariff{Name: "traffic-tariff", TrafficStrategy: model.StrategyOverwrite}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatalf("create tariff: %v", err)
	}
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})

	group := model.ClientGroup{Name: "traf-group", TariffID: &tariff.Id}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	startedAt := int64(1700000000000)

	t.Run("resolved=true writes tariff-effective values", func(t *testing.T) {
		rec := model.ClientRecord{
			Email:      "resolved@x.com",
			Group:      "traf-group",
			Enable:     true,
			TotalGB:    50,
			ExpiryTime: 999,
		}
		db.Create(&rec)
		db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt})
		seedClientRow(t, "resolved@x.com", 1, 1000, 2000, 50)

		rewriteTrafficForClients(db, []model.ClientRecord{rec}, true)

		var traf xray.ClientTraffic
		if err := db.Where("email = ?", "resolved@x.com").First(&traf).Error; err != nil {
			t.Fatalf("load traffic: %v", err)
		}
		wantTotal := int64(100) * bytesPerGB
		if traf.Total != wantTotal {
			t.Errorf("total = %d, want %d", traf.Total, wantTotal)
		}
		wantExpiry := startedAt + 60*86400*1000
		if traf.ExpiryTime != wantExpiry {
			t.Errorf("expiry_time = %d, want %d", traf.ExpiryTime, wantExpiry)
		}
	})

	t.Run("resolved=true skips overridden totalGB", func(t *testing.T) {
		rec := model.ClientRecord{
			Email:      "overridden-total@x.com",
			Group:      "traf-group",
			Enable:     true,
			TotalGB:    77,
			ExpiryTime: 999,
		}
		db.Create(&rec)
		gbOverride := int64(20)
		db.Create(&model.ClientTariff{
			ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt,
			TotalGBOverride: &gbOverride,
		})
		seedClientRow(t, "overridden-total@x.com", 1, 0, 0, 20)

		rewriteTrafficForClients(db, []model.ClientRecord{rec}, true)

		var traf xray.ClientTraffic
		db.Where("email = ?", "overridden-total@x.com").First(&traf)
		if traf.Total != 20 {
			t.Errorf("total = %d, want 20 (unchanged seed value when overridden)", traf.Total)
		}
	})

	t.Run("resolved=true skips overridden expiryTime", func(t *testing.T) {
		rec := model.ClientRecord{
			Email:      "overridden-expiry@x.com",
			Group:      "traf-group",
			Enable:     true,
			TotalGB:    50,
			ExpiryTime: 888,
		}
		db.Create(&rec)
		expOverride := int64(0)
		db.Create(&model.ClientTariff{
			ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt,
			ExpiryTimeOverride: &expOverride,
		})
		seedClientRow(t, "overridden-expiry@x.com", 1, 0, 0, 50)

		rewriteTrafficForClients(db, []model.ClientRecord{rec}, true)

		var traf xray.ClientTraffic
		db.Where("email = ?", "overridden-expiry@x.com").First(&traf)
		if traf.ExpiryTime != 0 {
			t.Errorf("expiry_time = %d, want 0 (unchanged seed value when overridden)", traf.ExpiryTime)
		}
	})

	t.Run("resolved=false writes raw client values", func(t *testing.T) {
		rec := model.ClientRecord{
			Email:      "raw@x.com",
			Group:      "traf-group",
			Enable:     true,
			TotalGB:    50,
			ExpiryTime: 999,
		}
		db.Create(&rec)
		seedClientRow(t, "raw@x.com", 1, 0, 0, 999)

		rewriteTrafficForClients(db, []model.ClientRecord{rec}, false)

		var traf xray.ClientTraffic
		db.Where("email = ?", "raw@x.com").First(&traf)
		if traf.Total != 50 {
			t.Errorf("total = %d, want 50 (raw)", traf.Total)
		}
		if traf.ExpiryTime != 999 {
			t.Errorf("expiry_time = %d, want 999 (raw)", traf.ExpiryTime)
		}
	})
}

func TestRefreshTrafficForGroup(t *testing.T) {
	initTariffTestDB(t)
	db := database.GetDB()

	profile := model.Profile{Name: "refresh-p", Traffic: int64Ptr(30), ExpiryDays: intPtr(90)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "refresh-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})

	startedAt := int64(1700000000000)

	t.Run("updates traffic for tariff-bound group", func(t *testing.T) {
		group := model.ClientGroup{Name: "bound-group", TariffID: &tariff.Id}
		db.Create(&group)

		rec := model.ClientRecord{Email: "bound@x.com", Group: "bound-group", Enable: true, TotalGB: 10, ExpiryTime: 1}
		db.Create(&rec)
		db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt})
		seedClientRow(t, "bound@x.com", 1, 0, 0, 10)

		svc := &TariffService{}
		svc.RefreshTrafficForGroup("bound-group")

		var traf xray.ClientTraffic
		db.Where("email = ?", "bound@x.com").First(&traf)
		wantTotal := int64(30) * bytesPerGB
		if traf.Total != wantTotal {
			t.Errorf("total = %d, want %d", traf.Total, wantTotal)
		}
	})

	t.Run("does nothing for non-tariff group", func(t *testing.T) {
		db.Create(&model.ClientGroup{Name: "free-group"})
		rec := model.ClientRecord{Email: "free@x.com", Group: "free-group", Enable: true, TotalGB: 5}
		db.Create(&rec)
		seedClientRow(t, "free@x.com", 1, 0, 0, 5)

		svc := &TariffService{}
		svc.RefreshTrafficForGroup("free-group")

		var traf xray.ClientTraffic
		db.Where("email = ?", "free@x.com").First(&traf)
		if traf.Total != 5 {
			t.Errorf("total = %d, want 5 (unchanged for non-tariff group)", traf.Total)
		}
	})

	t.Run("does nothing for non-existent group", func(t *testing.T) {
		svc := &TariffService{}
		// must not panic
		svc.RefreshTrafficForGroup("no-such-group")
	})
}

func TestRefreshTrafficForGroupReset(t *testing.T) {
	initTariffTestDB(t)
	db := database.GetDB()

	profile := model.Profile{Name: "reset-p", Traffic: int64Ptr(200)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "reset-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})

	startedAt := int64(1700000000000)

	group := model.ClientGroup{Name: "reset-group", TariffID: &tariff.Id}
	db.Create(&group)

	// Overridden client — Reset must skip it, same as resolved path
	rec := model.ClientRecord{Email: "reset@x.com", Group: "reset-group", Enable: true, TotalGB: 10, ExpiryTime: 1}
	db.Create(&rec)
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt})
	seedClientRow(t, "reset@x.com", 1, 0, 0, 200) // currently at tariff-effective 200GB

	svc := &TariffService{}
	svc.RefreshTrafficForGroupReset("reset-group")

	var traf xray.ClientTraffic
	db.Where("email = ?", "reset@x.com").First(&traf)
	// RefreshTrafficForGroupReset writes raw TotalGB (10) because resolved=false
	if traf.Total != 10 {
		t.Errorf("total = %d, want 10 (raw client value after reset)", traf.Total)
	}
}

func TestTariffService_RefreshTariffTraffic(t *testing.T) {
	initTariffTestDB(t)
	db := database.GetDB()

	profile := model.Profile{Name: "rt-p", Traffic: int64Ptr(40)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "rt-t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})

	startedAt := int64(1700000000000)

	groupA := model.ClientGroup{Name: "rta", TariffID: &tariff.Id}
	groupB := model.ClientGroup{Name: "rtb", TariffID: &tariff.Id}
	db.Create(&groupA)
	db.Create(&groupB)

	rec1 := model.ClientRecord{Email: "rta@x.com", Group: "rta", Enable: true, TotalGB: 1}
	rec2 := model.ClientRecord{Email: "rtb@x.com", Group: "rtb", Enable: true, TotalGB: 2}
	db.Create(&rec1)
	db.Create(&rec2)
	db.Create(&model.ClientTariff{ClientID: rec1.Id, TariffID: tariff.Id, StartedAt: startedAt})
	db.Create(&model.ClientTariff{ClientID: rec2.Id, TariffID: tariff.Id, StartedAt: startedAt})
	seedClientRow(t, "rta@x.com", 1, 0, 0, 1)
	seedClientRow(t, "rtb@x.com", 1, 0, 0, 2)

	svc := &TariffService{}
	svc.Update(tariff.Id, "rt-t-renamed", TariffStrategies{TrafficStrategy: model.StrategyOverwrite})

	wantTotal := int64(40) * bytesPerGB
	for _, email := range []string{"rta@x.com", "rtb@x.com"} {
		var traf xray.ClientTraffic
		db.Where("email = ?", email).First(&traf)
		if traf.Total != wantTotal {
			t.Errorf("%s total = %d, want %d", email, traf.Total, wantTotal)
		}
	}
}

func TestTariffService_CreateDuplicateName(t *testing.T) {
	initTariffTestDB(t)
	svc := &TariffService{}

	_, err := svc.Create("Gold", TariffStrategies{})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err = svc.Create("Gold", TariffStrategies{})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("err = %v, want UNIQUE constraint error", err)
	}
}

func TestRewriteTrafficForClients_NoTrafficProfile(t *testing.T) {
	initTariffTestDB(t)
	db := database.GetDB()

	// Profile with NO traffic and NO expiry — chain resolves HasTraffic=false, HasExpiryDays=false.
	profile := model.Profile{Name: "empty-p", Traffic: nil, ExpiryDays: nil}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create profile: %v", err)
	}
	tariff := model.Tariff{Name: "empty-t", TrafficStrategy: model.StrategyOverwrite}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatalf("create tariff: %v", err)
	}
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "empty-group", TariffID: &tariff.Id}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	startedAt := int64(1700000000000)
	rec := model.ClientRecord{
		Email:      "empty@x.com",
		Group:      "empty-group",
		Enable:     true,
		TotalGB:    77,
		ExpiryTime: 888,
	}
	db.Create(&rec)
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: startedAt})
	seedClientRow(t, "empty@x.com", 1, 0, 0, 10)

	rewriteTrafficForClients(db, []model.ClientRecord{rec}, true)

	var traf xray.ClientTraffic
	db.Where("email = ?", "empty@x.com").First(&traf)
	if traf.Total != 77 {
		t.Errorf("total = %d, want 77 (client own value when chain HasTraffic=false)", traf.Total)
	}
	if traf.ExpiryTime != 888 {
		t.Errorf("expiry_time = %d, want 888 (client own value when chain HasExpiryDays=false)", traf.ExpiryTime)
	}
}
