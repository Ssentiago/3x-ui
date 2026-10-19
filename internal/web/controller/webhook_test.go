package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
)

func setupWebhookRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	engine := gin.New()
	NewWebhookController(engine.Group("/panel/api"))
	return engine
}

func postJSON(t *testing.T, engine *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func TestWebhookCreateListUpdateDelete(t *testing.T) {
	engine := setupWebhookRouter(t)

	w := postJSON(t, engine, "/panel/api/webhooks/create", map[string]any{
		"name":          "n8n",
		"url":           "https://example.com/hook",
		"secret":        "sign-me",
		"enabled":       true,
		"enabledEvents": "login.attempt",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		Success bool `json:"success"`
		Obj     struct {
			Id        int  `json:"id"`
			HasSecret bool `json:"hasSecret"`
		} `json:"obj"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.Success || created.Obj.Id == 0 || !created.Obj.HasSecret {
		t.Fatalf("create response = %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sign-me")) {
		t.Fatal("create response leaked the secret")
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panel/api/webhooks", nil)
	engine.ServeHTTP(w, req)
	if bytes.Contains(w.Body.Bytes(), []byte("sign-me")) {
		t.Fatal("list leaked the secret")
	}

	w = postJSON(t, engine, "/panel/api/webhooks/update/1", map[string]any{
		"name":    "renamed",
		"url":     "https://example.com/hook",
		"enabled": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sign-me")) {
		t.Fatal("update response leaked the secret")
	}

	w = postJSON(t, engine, "/panel/api/webhooks/delete/1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookTestClassifiesFailureAsI18nKey(t *testing.T) {
	engine := setupWebhookRouter(t)
	w := postJSON(t, engine, "/panel/api/webhooks/test", map[string]any{
		"name":         "draft",
		"url":          "http://127.0.0.1:1/hook",
		"secret":       "s",
		"allowPrivate": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("test status = %d body=%s", w.Code, w.Body.String())
	}
	var result struct {
		Success bool   `json:"success"`
		Stage   string `json:"stage"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatalf("unreachable target must fail: %s", w.Body.String())
	}
	if result.Msg != "webhookErrorConnRefused" {
		t.Fatalf("msg = %q, want webhookErrorConnRefused (classified i18n key)", result.Msg)
	}
	if result.Stage != "send" {
		t.Fatalf("stage = %q, want send", result.Stage)
	}
}
