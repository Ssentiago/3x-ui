package service

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func initPolicyTestDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

func TestPolicyCRUD(t *testing.T) {
	initPolicyTestDB(t)
	db := database.GetDB()
	svc := &PolicyService{}

	created, err := svc.Create(db, PolicyInput{
		Name:       "Gold",
		TrafficGB:  ptrInt64(50),
		ExpiryDays: ptrInt(30),
		LimitIP:    ptrInt(3),
		InboundIds: []int{1, 2},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Id == 0 {
		t.Fatalf("expected non-zero id")
	}
	if created.TrafficGB == nil || *created.TrafficGB != 50 {
		t.Fatalf("TrafficGB = %v, want 50", created.TrafficGB)
	}
	if len(created.InboundIds) != 2 {
		t.Fatalf("InboundIds = %v, want [1 2]", created.InboundIds)
	}

	_, err = svc.Create(db, PolicyInput{Name: "Gold"})
	if err == nil {
		t.Fatalf("expected duplicate name error")
	}

	if err := svc.Update(db, created.Id, PolicyInput{Name: "Platinum", TrafficGB: ptrInt64(100)}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := svc.Get(db, created.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Platinum" {
		t.Fatalf("Name = %q, want Platinum", got.Name)
	}
	if got.TrafficGB == nil || *got.TrafficGB != 100 {
		t.Fatalf("TrafficGB = %v, want 100", got.TrafficGB)
	}

	list, err := svc.List(db)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	if err := svc.Delete(db, created.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(db, created.Id); err == nil {
		t.Fatalf("expected not found after delete")
	}
}

func TestExceptionsCRUD(t *testing.T) {
	initPolicyTestDB(t)
	db := database.GetDB()

	if err := SetExceptions(db, "vip", []string{"alice", "bob", "alice"}); err != nil {
		t.Fatalf("SetExceptions: %v", err)
	}
	ex, err := ListExceptions(db, "vip")
	if err != nil {
		t.Fatalf("ListExceptions: %v", err)
	}
	if len(ex) != 2 {
		t.Fatalf("ListExceptions = %v, want 2 unique", ex)
	}

	if err := AddException(db, "vip", "carol"); err != nil {
		t.Fatalf("AddException: %v", err)
	}
	if err := AddException(db, "vip", "carol"); err != nil {
		t.Fatalf("AddException idempotent: %v", err)
	}
	ex, _ = ListExceptions(db, "vip")
	if len(ex) != 3 {
		t.Fatalf("after AddException len = %d, want 3", len(ex))
	}

	if err := RemoveException(db, "vip", "alice"); err != nil {
		t.Fatalf("RemoveException: %v", err)
	}
	ex, _ = ListExceptions(db, "vip")
	if len(ex) != 2 {
		t.Fatalf("after RemoveException len = %d, want 2", len(ex))
	}

	if err := SetExceptions(db, "vip", nil); err != nil {
		t.Fatalf("SetExceptions clear: %v", err)
	}
	ex, _ = ListExceptions(db, "vip")
	if len(ex) != 0 {
		t.Fatalf("after clear len = %d, want 0", len(ex))
	}
}

func TestPreviewCountsChanges(t *testing.T) {
	initPolicyTestDB(t)
	db := database.GetDB()
	svc := &PolicyService{}

	p, err := svc.Create(db, PolicyInput{
		Name:       "Gold",
		TrafficGB:  ptrInt64(50),
		ExpiryDays: ptrInt(30),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	clients := []model.ClientRecord{
		{Email: "a@test.com", Group: "vip", TotalGB: 10 * policyBytesPerGB, ExpiryTime: 100},
		{Email: "b@test.com", Group: "vip", TotalGB: 50 * policyBytesPerGB, ExpiryTime: 0},
		{Email: "c@test.com", Group: "vip", TotalGB: 1 * policyBytesPerGB, ExpiryTime: 200},
	}
	for i := range clients {
		if err := db.Create(&clients[i]).Error; err != nil {
			t.Fatalf("create client %d: %v", i, err)
		}
	}
	if err := SetExceptions(db, "vip", []string{"c@test.com"}); err != nil {
		t.Fatalf("SetExceptions: %v", err)
	}

	preview, err := Preview(db, "vip", p.Id)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.TotalClients != 3 {
		t.Fatalf("TotalClients = %d, want 3", preview.TotalClients)
	}
	if preview.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", preview.Skipped)
	}
	if preview.Changed != 2 {
		t.Fatalf("Changed = %d, want 2", preview.Changed)
	}
	if preview.FieldChanges["totalGB"] == 0 {
		t.Fatalf("FieldChanges[totalGB] = 0, want > 0")
	}
}

func TestApplyWritesFields(t *testing.T) {
	initPolicyTestDB(t)
	db := database.GetDB()
	svc := &PolicyService{}

	p, err := svc.Create(db, PolicyInput{
		Name:       "Gold",
		TrafficGB:  ptrInt64(50),
		ExpiryDays: ptrInt(30),
		LimitIP:    ptrInt(5),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec := model.ClientRecord{Email: "a@test.com", Group: "vip", TotalGB: 1, ExpiryTime: 1, LimitIP: 1}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	skipRec := model.ClientRecord{Email: "b@test.com", Group: "vip", TotalGB: 1, ExpiryTime: 1, LimitIP: 1}
	if err := db.Create(&skipRec).Error; err != nil {
		t.Fatalf("create skip client: %v", err)
	}
	if err := SetExceptions(db, "vip", []string{"b@test.com"}); err != nil {
		t.Fatalf("SetExceptions: %v", err)
	}

	var clientSvc ClientService
	var inboundSvc InboundService
	result, _, err := Apply(db, &clientSvc, &inboundSvc, "vip", p.Id)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Changed != 1 {
		t.Fatalf("Changed = %d, want 1", result.Changed)
	}

	var updated model.ClientRecord
	if err := db.Where("email = ?", "a@test.com").First(&updated).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if updated.TotalGB != 50*policyBytesPerGB {
		t.Fatalf("TotalGB = %d, want %d", updated.TotalGB, 50*policyBytesPerGB)
	}
	if updated.LimitIP != 5 {
		t.Fatalf("LimitIP = %d, want 5", updated.LimitIP)
	}

	var skipped model.ClientRecord
	if err := db.Where("email = ?", "b@test.com").First(&skipped).Error; err != nil {
		t.Fatalf("reload skipped: %v", err)
	}
	if skipped.TotalGB != 1 {
		t.Fatalf("skipped TotalGB = %d, want 1 (unchanged)", skipped.TotalGB)
	}
}

func ptrInt(v int) *int         { return &v }
func ptrInt64(v int64) *int64  { return &v }

func TestDiffKeepsOriginalInboundIds(t *testing.T) {
	p := &model.Policy{
		InboundIds: `[3,4]`,
	}
	clients := []clientPolicyState{
		{Email: "a@test.com", InboundIds: []int{1, 2}},
	}
	changed, fieldChanges, changes, _ := diffPolicyClients(p, clients, nil, 0)
	if len(changed) != 1 {
		t.Fatalf("changed len = %d, want 1", len(changed))
	}
	if fieldChanges["inbounds"] != 1 {
		t.Fatalf("fieldChanges[inbounds] = %d, want 1", fieldChanges["inbounds"])
	}
	if len(changes) != 1 {
		t.Fatalf("changes len = %d, want 1", len(changes))
	}
	if !equalIntSlices(changed[0].InboundIds, []int{1, 2}) {
		t.Fatalf("changed[0].InboundIds = %v, want original [1 2]", changed[0].InboundIds)
	}
}
