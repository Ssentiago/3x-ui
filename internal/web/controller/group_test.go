package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	xuilogger "github.com/mhsanaei/3x-ui/v3/internal/logger"
)

func newTariffTestDB(t *testing.T) {
	t.Helper()
	xuilogger.InitLogger(logging.ERROR)
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
}

type tariffEnvelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

func doTariffReq(t *testing.T, engine *gin.Engine, method, path string, body any) tariffEnvelope {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s: status %d, body=%s", method, path, w.Code, w.Body.String())
	}
	var env tariffEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: decode envelope: %v body=%s", method, path, err, w.Body.String())
	}
	return env
}

func newGroupTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	engine := gin.New()
	g := engine.Group("/panel/api")
	NewGroupController(g)
	return engine
}

// --- Tariff endpoints ---

func TestTariffController_CRUD(t *testing.T) {
	newTariffTestDB(t)
	engine := newGroupTestEngine(t)

	t.Run("create tariff returns success", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/tariffs/create", map[string]string{
			"name":            "Gold",
			"trafficStrategy": "sum",
			"inboundStrategy": "union",
		})
		if !env.Success {
			t.Fatalf("create failed: msg=%s", env.Msg)
		}
	})

	t.Run("create tariff with empty name returns error", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/tariffs/create", map[string]string{
			"name": "",
		})
		if env.Success {
			t.Fatal("expected failure for empty name")
		}
	})

	t.Run("list tariffs returns created tariff", func(t *testing.T) {
		env := doTariffReq(t, engine, "GET", "/panel/api/tariffs", nil)
		if !env.Success {
			t.Fatalf("list failed: msg=%s", env.Msg)
		}
		var list []map[string]any
		if err := json.Unmarshal(env.Obj, &list); err != nil {
			t.Fatalf("unmarshal list: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("list length = %d, want 1", len(list))
		}
		if list[0]["name"] != "Gold" {
			t.Errorf("name = %q, want Gold", list[0]["name"])
		}
	})

	t.Run("get tariff by id returns tariff with resolved", func(t *testing.T) {
		env := doTariffReq(t, engine, "GET", "/panel/api/tariffs/1", nil)
		if !env.Success {
			t.Fatalf("get failed: msg=%s", env.Msg)
		}
		var tmap map[string]any
		if err := json.Unmarshal(env.Obj, &tmap); err != nil {
			t.Fatalf("unmarshal tariff: %v", err)
		}
		if tmap["id"] == nil {
			t.Error("id is missing from response")
		}
		if tmap["resolved"] == nil {
			t.Error("resolved field missing — tariff Get must include resolved preview")
		}
	})

	t.Run("update tariff changes name", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/tariffs/1/update", map[string]string{
			"name":            "Platinum",
			"trafficStrategy": "overwrite",
		})
		if !env.Success {
			t.Fatalf("update failed: msg=%s", env.Msg)
		}

		env2 := doTariffReq(t, engine, "GET", "/panel/api/tariffs/1", nil)
		var tmap map[string]any
		json.Unmarshal(env2.Obj, &tmap)
		if tmap["name"] != "Platinum" {
			t.Errorf("name = %q, want Platinum", tmap["name"])
		}
	})

	t.Run("set tariff profiles succeeds", func(t *testing.T) {
		db := database.GetDB()
		profile := model.Profile{Name: "BASE", Traffic: intPtrCtrl(50)}
		db.Create(&profile)

		env := doTariffReq(t, engine, "POST", "/panel/api/tariffs/1/profiles", map[string]any{
			"profileIds": []map[string]int{
				{"id": profile.Id, "position": 0},
			},
		})
		if !env.Success {
			t.Fatalf("set profiles failed: msg=%s", env.Msg)
		}
	})

	t.Run("delete tariff succeeds when no groups", func(t *testing.T) {
		doTariffReq(t, engine, "POST", "/panel/api/tariffs/create", map[string]string{
			"name": "Silver",
		})
		env := doTariffReq(t, engine, "POST", "/panel/api/tariffs/2/delete", nil)
		if !env.Success {
			t.Fatalf("delete failed: msg=%s", env.Msg)
		}
	})

	t.Run("delete tariff fails when used by group", func(t *testing.T) {
		doTariffReq(t, engine, "POST", "/panel/api/tariffs/create", map[string]string{
			"name": "UsedTariff",
		})
		db := database.GetDB()
		var tariff model.Tariff
		db.Where("name = ?", "UsedTariff").First(&tariff)
		db.Create(&model.ClientGroup{Name: "vip", TariffID: &tariff.Id})

		env := doTariffReq(t, engine, "POST", "/panel/api/tariffs/"+fmt.Sprint(tariff.Id)+"/delete", nil)
		if env.Success {
			t.Fatal("expected failure when tariff is used by groups")
		}
	})
}

// --- Profile endpoints ---

func TestProfileController_CRUD(t *testing.T) {
	newTariffTestDB(t)
	engine := newGroupTestEngine(t)

	t.Run("create profile succeeds", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/profiles/create", map[string]any{
			"name":       "BASE",
			"traffic":    100,
			"expiryDays": 30,
			"limitIp":    3,
			"inboundIds": []int{1, 2},
		})
		if !env.Success {
			t.Fatalf("create failed: msg=%s", env.Msg)
		}
	})

	t.Run("create profile with empty name fails", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/profiles/create", map[string]string{
			"name": "",
		})
		if env.Success {
			t.Fatal("expected failure for empty name")
		}
	})

	t.Run("list profiles returns created profile", func(t *testing.T) {
		env := doTariffReq(t, engine, "GET", "/panel/api/profiles", nil)
		if !env.Success {
			t.Fatalf("list failed: msg=%s", env.Msg)
		}
		var list []map[string]any
		json.Unmarshal(env.Obj, &list)
		if len(list) != 1 {
			t.Fatalf("list length = %d, want 1", len(list))
		}
	})

	t.Run("get profile returns full data", func(t *testing.T) {
		env := doTariffReq(t, engine, "GET", "/panel/api/profiles/1", nil)
		if !env.Success {
			t.Fatalf("get failed: msg=%s", env.Msg)
		}
		var pmap map[string]any
		json.Unmarshal(env.Obj, &pmap)
		if pmap["name"] != "BASE" {
			t.Errorf("name = %q, want BASE", pmap["name"])
		}
	})

	t.Run("delete unused profile succeeds", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/profiles/1/delete", nil)
		if !env.Success {
			t.Fatalf("delete failed: msg=%s", env.Msg)
		}
	})
}

// --- Override / ReturnToTariff ---

func TestOverrideField_Controller(t *testing.T) {
	newTariffTestDB(t)
	db := database.GetDB()
	engine := newGroupTestEngine(t)

	profile := model.Profile{Name: "ov-p", Traffic: intPtrCtrl(200), LimitIP: intPtrCtrlI(10), ExpiryDays: intPtrCtrlI(60)}
	db.Create(&profile)
	tariff := model.Tariff{Name: "ov-t", TrafficStrategy: "overwrite"}
	db.Create(&tariff)
	db.Create(&model.TariffProfile{TariffID: tariff.Id, ProfileID: profile.Id, Position: 0})
	group := model.ClientGroup{Name: "ov-group", TariffID: &tariff.Id}
	db.Create(&group)

	client := model.ClientRecord{
		Email:      "override-ctrl@x.com",
		Group:      "ov-group",
		TotalGB:    10,
		LimitIP:    1,
		ExpiryTime: 999,
	}
	db.Create(&client)
	db.Create(&model.ClientTariff{ClientID: client.Id, TariffID: tariff.Id, StartedAt: int64(1700000000000)})

	t.Run("override totalGB returns success", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/overrideField", map[string]string{
			"email": "override-ctrl@x.com",
			"field": "totalGB",
		})
		if !env.Success {
			t.Fatalf("overrideField failed: msg=%s", env.Msg)
		}
		var rec model.ClientRecord
		db.Where("email = ?", "override-ctrl@x.com").First(&rec)
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", rec.Id).First(&ct)
		if ct.TotalGBOverride == nil {
			t.Error("TotalGBOverride should not be nil after override")
		}
	})

	t.Run("returnToTariff totalGB returns success", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/returnToTariff", map[string]string{
			"email": "override-ctrl@x.com",
			"field": "totalGB",
		})
		if !env.Success {
			t.Fatalf("returnToTariff failed: msg=%s", env.Msg)
		}
		var rec model.ClientRecord
		db.Where("email = ?", "override-ctrl@x.com").First(&rec)
		var ct model.ClientTariff
		db.Where("client_id = ? AND ended_at IS NULL", rec.Id).First(&ct)
		if ct.TotalGBOverride != nil {
			t.Error("TotalGBOverride should be nil after return")
		}
	})

	t.Run("unknown field returns error", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/overrideField", map[string]string{
			"email": "override-ctrl@x.com",
			"field": "bogus",
		})
		if env.Success {
			t.Fatal("expected failure for unknown field")
		}
	})
}

// --- Group endpoints ---

func TestGroupController_BasicCRUD(t *testing.T) {
	newTariffTestDB(t)
	engine := newGroupTestEngine(t)

	t.Run("create group without tariff succeeds", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/groups/create", map[string]any{
			"name":     "free-group",
			"tariffId": nil,
		})
		if !env.Success {
			t.Fatalf("create group failed: msg=%s", env.Msg)
		}
	})

	t.Run("list groups returns created group", func(t *testing.T) {
		env := doTariffReq(t, engine, "GET", "/panel/api/groups", nil)
		if !env.Success {
			t.Fatalf("list groups failed: msg=%s", env.Msg)
		}
		var list []map[string]any
		json.Unmarshal(env.Obj, &list)
		if len(list) != 1 {
			t.Fatalf("list length = %d, want 1", len(list))
		}
		if list[0]["name"] != "free-group" {
			t.Errorf("name = %q, want free-group", list[0]["name"])
		}
	})

	t.Run("delete group succeeds", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/groups/delete", map[string]string{
			"name": "free-group",
		})
		if !env.Success {
			t.Fatalf("delete group failed: msg=%s", env.Msg)
		}
	})
}

func TestGroupController_RenameAndBulk(t *testing.T) {
	newTariffTestDB(t)
	db := database.GetDB()
	engine := newGroupTestEngine(t)

	doTariffReq(t, engine, "POST", "/panel/api/groups/create", map[string]any{
		"name":     "old-name",
		"tariffId": nil,
	})

	client := model.ClientRecord{Email: "rename@x.com", Group: "old-name", Enable: true}
	db.Create(&client)

	t.Run("rename group succeeds", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/groups/rename", map[string]any{
			"oldName":  "old-name",
			"newName":  "new-name",
			"tariffId": nil,
		})
		if !env.Success {
			t.Fatalf("rename failed: msg=%s", env.Msg)
		}
		var grp model.ClientGroup
		if err := db.Where("name = ?", "new-name").First(&grp).Error; err != nil {
			t.Fatalf("renamed group not found: %v", err)
		}
		var rec model.ClientRecord
		db.Where("email = ?", "rename@x.com").First(&rec)
		if rec.Group != "new-name" {
			t.Errorf("client group = %q, want new-name", rec.Group)
		}
	})

	t.Run("get emails by group name", func(t *testing.T) {
		env := doTariffReq(t, engine, "GET", "/panel/api/groups/new-name/emails", nil)
		if !env.Success {
			t.Fatalf("emails failed: msg=%s", env.Msg)
		}
		var emails []string
		json.Unmarshal(env.Obj, &emails)
		if len(emails) != 1 || emails[0] != "rename@x.com" {
			t.Errorf("emails = %v, want [rename@x.com]", emails)
		}
	})
}

func TestGroupController_ResetTrafficAndBulk(t *testing.T) {
	newTariffTestDB(t)
	db := database.GetDB()
	engine := newGroupTestEngine(t)

	doTariffReq(t, engine, "POST", "/panel/api/groups/create", map[string]any{
		"name":     "bulk-group",
		"tariffId": nil,
	})

	t.Run("reset group traffic succeeds", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/groups/resetTraffic", map[string]string{
			"name": "bulk-group",
		})
		if !env.Success {
			t.Fatalf("resetTraffic failed: msg=%s", env.Msg)
		}
	})

	t.Run("bulk add clients to group", func(t *testing.T) {
		db.Create(&model.ClientRecord{Email: "bulk1@x.com", Enable: true})
		db.Create(&model.ClientRecord{Email: "bulk2@x.com", Enable: true})

		env := doTariffReq(t, engine, "POST", "/panel/api/groups/bulkAdd", map[string]any{
			"emails": []string{"bulk1@x.com", "bulk2@x.com"},
			"group":  "bulk-group",
		})
		if !env.Success {
			t.Fatalf("bulkAdd failed: msg=%s", env.Msg)
		}

		var recs []model.ClientRecord
		db.Where("group_name = ?", "bulk-group").Find(&recs)
		if len(recs) != 2 {
			t.Errorf("clients in group = %d, want 2", len(recs))
		}
	})

	t.Run("bulk remove clients from group", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/groups/bulkRemove", map[string]any{
			"emails": []string{"bulk1@x.com"},
		})
		if !env.Success {
			t.Fatalf("bulkRemove failed: msg=%s", env.Msg)
		}

		var rec1 model.ClientRecord
		db.Where("email = ?", "bulk1@x.com").First(&rec1)
		if rec1.Group != "" {
			t.Errorf("bulk1 group = %q, want empty after remove", rec1.Group)
		}

		var rec2 model.ClientRecord
		db.Where("email = ?", "bulk2@x.com").First(&rec2)
		if rec2.Group != "bulk-group" {
			t.Errorf("bulk2 group = %q, want bulk-group", rec2.Group)
		}
	})
}

func TestGroupController_ResetTariff(t *testing.T) {
	newTariffTestDB(t)
	db := database.GetDB()
	engine := newGroupTestEngine(t)

	tariff := model.Tariff{Name: "reset-tariff-t", TrafficStrategy: "overwrite"}
	db.Create(&tariff)

	doTariffReq(t, engine, "POST", "/panel/api/groups/create", map[string]any{
		"name":     "tariff-group",
		"tariffId": tariff.Id,
	})

	var grp model.ClientGroup
	db.Where("name = ?", "tariff-group").First(&grp)
	if grp.TariffID == nil || *grp.TariffID != tariff.Id {
		t.Fatalf("group tariff not set: %v", grp.TariffID)
	}

	t.Run("reset group tariff clears tariff_id", func(t *testing.T) {
		env := doTariffReq(t, engine, "POST", "/panel/api/groups/resetTariff", map[string]string{
			"name": "tariff-group",
		})
		if !env.Success {
			t.Fatalf("resetTariff failed: msg=%s", env.Msg)
		}

		db.First(&grp, grp.Id)
		if grp.TariffID != nil {
			t.Errorf("TariffID = %v, want nil after reset", grp.TariffID)
		}
	})
}

func intPtrCtrl(v int64) *int64 { return &v }

func intPtrCtrlI(v int) *int { return &v }
