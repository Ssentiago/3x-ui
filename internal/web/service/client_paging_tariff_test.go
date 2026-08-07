package service

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestListPagedTariffResolution(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	p := model.Profile{Name: "p", Traffic: int64Ptr(10), ExpiryDays: intPtr(30), LimitIP: intPtr(5)}
	db.Create(&p)
	tariff := model.Tariff{Name: "t", TrafficStrategy: model.StrategyOverwrite}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})

	grp := model.ClientGroup{Name: "paged-group", TariffID: &tariff.Id}
	db.Create(&grp)

	ts := int64(1700000000000)
	rec := model.ClientRecord{
		Email:      "paged-tariff@test.com",
		Group:      "paged-group",
		TotalGB:    50,
		LimitIP:    3,
		ExpiryTime: 1700000000000,
	}
	db.Create(&rec)
	db.Create(&model.ClientTariff{
		ClientID: rec.Id, TariffID: tariff.Id, StartedAt: ts,
	})

	rec2 := model.ClientRecord{
		Email:   "paged-raw@test.com",
		TotalGB: 100,
		LimitIP: 10,
	}
	db.Create(&rec2)

	var svc ClientService
	resp, err := svc.ListPaged(nil, nil, ClientPageParams{PageSize: 10})
	if err != nil {
		t.Fatalf("ListPaged: %v", err)
	}

	if len(resp.Items) < 2 {
		t.Fatalf("items = %d, want >= 2", len(resp.Items))
	}

	var tariffClient, rawClient *ClientSlim
	for i := range resp.Items {
		switch resp.Items[i].Email {
		case "paged-tariff@test.com":
			tariffClient = &resp.Items[i]
		case "paged-raw@test.com":
			rawClient = &resp.Items[i]
		}
	}

	if tariffClient == nil || rawClient == nil {
		t.Fatal("one or both test clients not found in ListPaged response")
	}

	wantGB := int64(10) * 1073741824 // 10GB in bytes
	if tariffClient.TotalGB != wantGB {
		t.Errorf("tariff client TotalGB = %d, want %d (10GB)", tariffClient.TotalGB, wantGB)
	}
	if tariffClient.LimitIP != 5 {
		t.Errorf("tariff client LimitIP = %d, want 5", tariffClient.LimitIP)
	}
	wantExpiry := ts + 30*86400*1000
	if tariffClient.ExpiryTime != wantExpiry {
		t.Errorf("tariff client ExpiryTime = %d, want %d", tariffClient.ExpiryTime, wantExpiry)
	}

	if rawClient.TotalGB != 100 {
		t.Errorf("raw client TotalGB = %d, want 100", rawClient.TotalGB)
	}
	if rawClient.LimitIP != 10 {
		t.Errorf("raw client LimitIP = %d, want 10", rawClient.LimitIP)
	}
}

func TestListPagedOverrideWins(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	p := model.Profile{Name: "p", Traffic: int64Ptr(10), LimitIP: intPtr(5)}
	db.Create(&p)
	tariff := model.Tariff{Name: "t"}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})

	grp := model.ClientGroup{Name: "ov-group", TariffID: &tariff.Id}
	db.Create(&grp)

	ts := int64(1700000000000)
	rec := model.ClientRecord{
		Email:   "overridden@test.com",
		Group:   "ov-group",
		TotalGB: 777,
		LimitIP: 99,
	}
	db.Create(&rec)
	db.Create(&model.ClientTariff{
		ClientID: rec.Id, TariffID: tariff.Id, StartedAt: ts,
		TotalGBOverride: int64Ptr(777), LimitIPOverride: intPtr(99),
	})

	var svc ClientService
	resp, err := svc.ListPaged(nil, nil, ClientPageParams{PageSize: 10})
	if err != nil {
		t.Fatalf("ListPaged: %v", err)
	}

	var client *ClientSlim
	for i := range resp.Items {
		if resp.Items[i].Email == "overridden@test.com" {
			client = &resp.Items[i]
			break
		}
	}
	if client == nil {
		t.Fatal("client not found")
	}
	if client.TotalGB != 777 {
		t.Errorf("TotalGB = %d, want 777 (override beats tariff)", client.TotalGB)
	}
	if client.LimitIP != 99 {
		t.Errorf("LimitIP = %d, want 99 (override beats tariff)", client.LimitIP)
	}
}

func TestListPagedSumTariff(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	p1 := model.Profile{Name: "p1", Traffic: int64Ptr(10)}
	db.Create(&p1)
	p2 := model.Profile{Name: "p2", Traffic: int64Ptr(5)}
	db.Create(&p2)
	tariff := model.Tariff{Name: "sum-tariff", TrafficStrategy: model.StrategySum}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p1.Id, Position: 0})
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p2.Id, Position: 1})

	grp := model.ClientGroup{Name: "sum-group", TariffID: &tariff.Id}
	db.Create(&grp)

	rec := model.ClientRecord{Email: "sum@test.com", Group: "sum-group", TotalGB: 999}
	db.Create(&rec)
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: 1700000000000})

	var svc ClientService
	resp, err := svc.ListPaged(nil, nil, ClientPageParams{PageSize: 10})
	if err != nil {
		t.Fatalf("ListPaged: %v", err)
	}

	for i := range resp.Items {
		if resp.Items[i].Email == "sum@test.com" {
			wantGB := int64(15) * 1073741824
			if resp.Items[i].TotalGB != wantGB {
				t.Errorf("sum TotalGB = %d, want %d (10+5 GB)", resp.Items[i].TotalGB, wantGB)
			}
			return
		}
	}
	t.Fatal("sum client not found")
}

func TestListPagedNoClientTariff(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	p := model.Profile{Name: "p", Traffic: int64Ptr(10), ExpiryDays: intPtr(30)}
	db.Create(&p)
	tariff := model.Tariff{Name: "t"}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})

	grp := model.ClientGroup{Name: "noct-group", TariffID: &tariff.Id}
	db.Create(&grp)

	rec := model.ClientRecord{
		Email:      "noct@test.com",
		Group:      "noct-group",
		TotalGB:    50,
		ExpiryTime: 1700000000000,
	}
	db.Create(&rec)

	var svc ClientService
	resp, err := svc.ListPaged(nil, nil, ClientPageParams{PageSize: 10})
	if err != nil {
		t.Fatalf("ListPaged: %v", err)
	}

	for i := range resp.Items {
		if resp.Items[i].Email == "noct@test.com" {
			if resp.Items[i].ExpiryTime != 1700000000000 {
				t.Errorf("ExpiryTime = %d, want 1700000000000 (raw, no started_at)", resp.Items[i].ExpiryTime)
			}
			wantGB := int64(10) * 1073741824
			if resp.Items[i].TotalGB != wantGB {
				t.Errorf("TotalGB = %d, want %d (tariff resolved even without started_at)", resp.Items[i].TotalGB, wantGB)
			}
			return
		}
	}
	t.Fatal("noct client not found")
}

func TestListPagedDeletedTariff(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	db := database.GetDB()

	p := model.Profile{Name: "p", Traffic: int64Ptr(10), LimitIP: intPtr(5)}
	db.Create(&p)
	tariff := model.Tariff{Name: "del-tariff"}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: p.Id, Position: 0})

	grp := model.ClientGroup{Name: "del-group", TariffID: &tariff.Id}
	db.Create(&grp)

	rec := model.ClientRecord{
		Email:      "del@test.com",
		Group:      "del-group",
		TotalGB:    777,
		LimitIP:    99,
		ExpiryTime: 1700000000000,
	}
	db.Create(&rec)
	db.Create(&model.ClientTariff{ClientID: rec.Id, TariffID: tariff.Id, StartedAt: 1700000000000})

	db.Delete(&tariff)
	db.Delete(&model.TariffProfile{}, "tariff_id = ?", tariff.Id)

	var svc ClientService
	resp, err := svc.ListPaged(nil, nil, ClientPageParams{PageSize: 10})
	if err != nil {
		t.Fatalf("ListPaged: %v", err)
	}

	for i := range resp.Items {
		if resp.Items[i].Email == "del@test.com" {
			if resp.Items[i].TotalGB != 777 {
				t.Errorf("TotalGB = %d, want 777 (raw, tariff deleted)", resp.Items[i].TotalGB)
			}
			if resp.Items[i].LimitIP != 99 {
				t.Errorf("LimitIP = %d, want 99 (raw, tariff deleted)", resp.Items[i].LimitIP)
			}
			return
		}
	}
	t.Fatal("del client not found")
}
