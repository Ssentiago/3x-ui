package webhook

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func setupEndpointTestDB(t *testing.T) {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
}

func TestListRedactsSecret(t *testing.T) {
	setupEndpointTestDB(t)
	svc := &EndpointService{}
	created, err := svc.Create(EndpointView{
		Name:          "n8n",
		Url:           "https://example.com/hook",
		Enabled:       true,
		EnabledEvents: "login.attempt",
	}, "signing-secret")
	if err != nil {
		t.Fatal(err)
	}
	if created.HasSecret != true {
		t.Fatal("HasSecret must report a stored secret")
	}
	list, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}
	if list[0].HasSecret != true {
		t.Fatal("list must report hasSecret")
	}
	row, err := svc.Load(created.Id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Secret != "signing-secret" {
		t.Fatalf("Load must return the secret for delivery, got %q", row.Secret)
	}
	// The wire view never carries the secret back.
	var stored model.WebhookEndpoint
	if err := database.GetDB().First(&stored, created.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Secret != "signing-secret" {
		t.Fatalf("stored secret = %q", stored.Secret)
	}
}

func TestUpdatePreservesBlankSecretUnlessCleared(t *testing.T) {
	setupEndpointTestDB(t)
	svc := &EndpointService{}
	created, err := svc.Create(EndpointView{
		Name: "n8n",
		Url:  "https://example.com/hook",
	}, "original-secret")
	if err != nil {
		t.Fatal(err)
	}

	// Blank secret + clearSecret=false must keep the stored one.
	if _, err := svc.Update(created.Id, EndpointView{
		Name: "renamed",
		Url:  "https://example.com/hook",
	}, "", false); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Load(created.Id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Secret != "original-secret" {
		t.Fatalf("secret after blank update = %q, want original", row.Secret)
	}
	if row.Name != "renamed" {
		t.Fatalf("name = %q", row.Name)
	}

	// Explicit clear wipes it.
	if _, err := svc.Update(created.Id, EndpointView{
		Name: "renamed",
		Url:  "https://example.com/hook",
	}, "", true); err != nil {
		t.Fatal(err)
	}
	row, err = svc.Load(created.Id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Secret != "" {
		t.Fatalf("secret after clear = %q, want empty", row.Secret)
	}
}

func TestUpdateReplacesSecretWhenProvided(t *testing.T) {
	setupEndpointTestDB(t)
	svc := &EndpointService{}
	created, err := svc.Create(EndpointView{Name: "a", Url: "https://example.com/h"}, "old")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(created.Id, EndpointView{Name: "a", Url: "https://example.com/h"}, "new", false); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Load(created.Id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Secret != "new" {
		t.Fatalf("secret = %q, want new", row.Secret)
	}
}

func TestCreateRejectsNonHTTPURL(t *testing.T) {
	setupEndpointTestDB(t)
	svc := &EndpointService{}
	if _, err := svc.Create(EndpointView{Name: "bad", Url: "file:///etc/passwd"}, ""); err == nil {
		t.Fatal("non-http scheme must be rejected")
	}
}
