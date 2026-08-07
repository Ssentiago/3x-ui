package service

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestListForInboundTariffResolution(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	ib := model.Inbound{
		Protocol: model.VLESS,
		Port:     443,
		Tag:      "test-inbound",
		Settings: `{"clients": []}`,
		Enable:   true,
	}
	if err := db.Create(&ib).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	// Tariff with 20GB, 7 IP, 60 days. Union strategy so clients appear
	// on their direct inbounds via step 1.
	p := model.Profile{Name: "p", Traffic: int64Ptr(20), ExpiryDays: intPtr(60), LimitIP: intPtr(7)}
	db.Create(&p)
	tariff := model.Tariff{Name: "batch-tariff", TrafficStrategy: model.StrategyOverwrite, InboundStrategy: model.StrategyUnion}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})

	grp := model.ClientGroup{Name: "batch-group", TariffID: &tariff.Id}
	db.Create(&grp)

	ts := int64(1700000000000)
	clients := []struct {
		email string
		rawGB int64
		rawIP int
	}{
		{"batch-a@test.com", 50, 3},
		{"batch-b@test.com", 100, 10},
	}
	for _, c := range clients {
		rec := model.ClientRecord{
			Email:    c.email,
			Group:    "batch-group",
			TotalGB:  c.rawGB,
			LimitIP:  c.rawIP,
			Password: "test",
		}
		db.Create(&rec)
		db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: ib.Id})
		db.Create(&model.ClientTariff{
			ClientID: rec.Id, TariffID: tariff.Id, StartedAt: ts,
		})
	}

	rawRec := model.ClientRecord{
		Email:    "batch-raw@test.com",
		TotalGB:  200,
		LimitIP:  20,
		Password: "test",
	}
	db.Create(&rawRec)
	db.Create(&model.ClientInbound{ClientId: rawRec.Id, InboundId: ib.Id})

	var svc ClientService
	result, err := svc.ListForInbound(nil, ib.Id)
	if err != nil {
		t.Fatalf("ListForInbound: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 clients, got %d", len(result))
	}

	byEmail := make(map[string]*model.Client)
	for i := range result {
		byEmail[result[i].Email] = &result[i]
	}

	// Tariff clients: should have tariff-resolved values.
	wantGB := int64(20) * 1073741824
	wantExpiry := ts + 60*86400*1000

	a := byEmail["batch-a@test.com"]
	if a == nil {
		t.Fatal("batch-a not found")
	}
	if a.TotalGB != wantGB {
		t.Errorf("batch-a TotalGB = %d, want %d", a.TotalGB, wantGB)
	}
	if a.LimitIP != 7 {
		t.Errorf("batch-a LimitIP = %d, want 7", a.LimitIP)
	}
	if a.ExpiryTime != wantExpiry {
		t.Errorf("batch-a ExpiryTime = %d, want %d", a.ExpiryTime, wantExpiry)
	}

	b := byEmail["batch-b@test.com"]
	if b == nil {
		t.Fatal("batch-b not found")
	}
	if b.TotalGB != wantGB {
		t.Errorf("batch-b TotalGB = %d, want %d", b.TotalGB, wantGB)
	}
	if b.LimitIP != 7 {
		t.Errorf("batch-b LimitIP = %d, want 7", b.LimitIP)
	}

	raw := byEmail["batch-raw@test.com"]
	if raw == nil {
		t.Fatal("batch-raw not found")
	}
	if raw.TotalGB != 200 {
		t.Errorf("raw TotalGB = %d, want 200", raw.TotalGB)
	}
	if raw.LimitIP != 20 {
		t.Errorf("raw LimitIP = %d, want 20", raw.LimitIP)
	}
}

func TestListForInboundOverrideWins(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	ib := model.Inbound{
		Protocol: model.VLESS,
		Port:     443,
		Tag:      "ov-inbound",
		Settings: `{"clients": []}`,
		Enable:   true,
	}
	db.Create(&ib)

	p := model.Profile{Name: "p", Traffic: int64Ptr(10), LimitIP: intPtr(5)}
	db.Create(&p)
	tariff := model.Tariff{Name: "ov-tariff", InboundStrategy: model.StrategyUnion}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})

	grp := model.ClientGroup{Name: "ov-group", TariffID: &tariff.Id}
	db.Create(&grp)

	rec := model.ClientRecord{
		Email:    "ov-client@test.com",
		Group:    "ov-group",
		TotalGB:  999,
		LimitIP:  88,
		Password: "test",
	}
	db.Create(&rec)
	db.Create(&model.ClientInbound{ClientId: rec.Id, InboundId: ib.Id})
	gbOverride := int64(999)
	ipOverride := 88
	db.Create(&model.ClientTariff{
		ClientID: rec.Id, TariffID: tariff.Id, StartedAt: 1700000000000,
		TotalGBOverride: &gbOverride, LimitIPOverride: &ipOverride,
	})

	var svc ClientService
	result, err := svc.ListForInbound(nil, ib.Id)
	if err != nil {
		t.Fatalf("ListForInbound: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 client, got %d", len(result))
	}
	if result[0].TotalGB != 999 {
		t.Errorf("TotalGB = %d, want 999 (override)", result[0].TotalGB)
	}
	if result[0].LimitIP != 88 {
		t.Errorf("LimitIP = %d, want 88 (override)", result[0].LimitIP)
	}
}

func TestListForInboundOverwriteStrategy(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	ib := model.Inbound{
		Protocol: model.VLESS,
		Port:     443,
		Tag:      "ow-inbound",
		Settings: `{"clients": []}`,
		Enable:   true,
	}
	db.Create(&ib)

	p := model.Profile{Name: "p", Traffic: int64Ptr(30), LimitIP: intPtr(12)}
	db.Create(&p)
	// We need to set InboundIds after knowing the inbound's ID.
	profileWithIbound := model.Profile{
		Name:       "p-with-ib",
		Traffic:    int64Ptr(30),
		LimitIP:    intPtr(12),
		InboundIds: intsToCsv([]int{ib.Id}),
	}
	db.Create(&profileWithIbound)
	tariff := model.Tariff{Name: "ow-tariff", InboundStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profileWithIbound.Id, Position: 0})

	grp := model.ClientGroup{Name: "ow-group", TariffID: &tariff.Id}
	db.Create(&grp)

	ts := int64(1700000000000)
	rec := model.ClientRecord{
		Email:    "ow-client@test.com",
		Group:    "ow-group",
		TotalGB:  555,
		LimitIP:  77,
		Password: "test",
	}
	db.Create(&rec)
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: ts})

	var svc ClientService
	result, err := svc.ListForInbound(nil, ib.Id)
	if err != nil {
		t.Fatalf("ListForInbound: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 client, got %d", len(result))
	}
	wantGB := int64(30) * 1073741824
	if result[0].TotalGB != wantGB {
		t.Errorf("TotalGB = %d, want %d (tariff via step2)", result[0].TotalGB, wantGB)
	}
	if result[0].LimitIP != 12 {
		t.Errorf("LimitIP = %d, want 12 (tariff via step2)", result[0].LimitIP)
	}
}

func intsToCsv(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	s := ""
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += strconv.Itoa(id)
	}
	return "[" + s + "]"
}
