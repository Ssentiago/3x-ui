package webhook

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// EndpointService manages WebhookEndpoint rows. Secrets are write-only: List
// and Get fill HasSecret and leave Secret empty.
type EndpointService struct{}

// EndpointView is the API shape: Secret is never serialized back to callers.
type EndpointView struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	Url              string `json:"url"`
	Enabled          bool   `json:"enabled"`
	EnabledEvents    string `json:"enabledEvents"`
	AllowPrivate     bool   `json:"allowPrivate"`
	BackupTransport  string `json:"backupTransport"`
	BackupAttachFile bool   `json:"backupAttachFile"`
	HasSecret        bool   `json:"hasSecret"`
}

// TestResult mirrors EmailService.SMTPTestResult for the test endpoint.
type TestResult struct {
	Success bool   `json:"success"`
	Stage   string `json:"stage"`
	Message string `json:"msg"`
}

func toView(m *model.WebhookEndpoint) EndpointView {
	return EndpointView{
		Id:               m.Id,
		Name:             m.Name,
		Url:              m.Url,
		Enabled:          m.Enabled,
		EnabledEvents:    m.EnabledEvents,
		AllowPrivate:     m.AllowPrivate,
		BackupTransport:  m.BackupTransport,
		BackupAttachFile: m.BackupAttachFile,
		HasSecret:        m.Secret != "",
	}
}

// List returns every endpoint with secrets redacted.
func (s *EndpointService) List() ([]EndpointView, error) {
	var rows []*model.WebhookEndpoint
	if err := database.GetDB().Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]EndpointView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toView(row))
	}
	return out, nil
}

// Get returns one endpoint with the secret redacted.
func (s *EndpointService) Get(id int) (EndpointView, error) {
	var row model.WebhookEndpoint
	if err := database.GetDB().First(&row, id).Error; err != nil {
		return EndpointView{}, err
	}
	return toView(&row), nil
}

// Load returns the stored endpoint including the secret, for delivery only.
func (s *EndpointService) Load(id int) (*model.WebhookEndpoint, error) {
	var row model.WebhookEndpoint
	if err := database.GetDB().First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// EnabledTargets returns every enabled endpoint with its secret, for fan-out.
func (s *EndpointService) EnabledTargets() ([]*model.WebhookEndpoint, error) {
	var rows []*model.WebhookEndpoint
	if err := database.GetDB().Where("enabled = ?", true).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Create validates the URL and stores a new endpoint.
func (s *EndpointService) Create(view EndpointView, secret string) (EndpointView, error) {
	if err := validateEndpoint(view, secret); err != nil {
		return EndpointView{}, err
	}
	row := &model.WebhookEndpoint{
		Name:             view.Name,
		Url:              strings.TrimSpace(view.Url),
		Secret:           secret,
		Enabled:          view.Enabled,
		EnabledEvents:    normalizeEvents(view.EnabledEvents),
		AllowPrivate:     view.AllowPrivate,
		BackupTransport:  normalizeTransport(view.BackupTransport),
		BackupAttachFile: view.BackupAttachFile,
	}
	if err := database.GetDB().Create(row).Error; err != nil {
		return EndpointView{}, err
	}
	return toView(row), nil
}

// Update mutates an endpoint. A blank secret preserves the stored one unless
// clearSecret is set — the same contract as SettingService secrets.
func (s *EndpointService) Update(id int, view EndpointView, secret string, clearSecret bool) (EndpointView, error) {
	if err := validateEndpoint(view, secret); err != nil {
		return EndpointView{}, err
	}
	var row model.WebhookEndpoint
	if err := database.GetDB().First(&row, id).Error; err != nil {
		return EndpointView{}, err
	}
	row.Name = view.Name
	row.Url = strings.TrimSpace(view.Url)
	row.Enabled = view.Enabled
	row.EnabledEvents = normalizeEvents(view.EnabledEvents)
	row.AllowPrivate = view.AllowPrivate
	row.BackupTransport = normalizeTransport(view.BackupTransport)
	row.BackupAttachFile = view.BackupAttachFile
	switch {
	case clearSecret:
		row.Secret = ""
	case secret != "":
		row.Secret = secret
	}
	if err := database.GetDB().Save(&row).Error; err != nil {
		return EndpointView{}, err
	}
	return toView(&row), nil
}

// Delete removes an endpoint.
func (s *EndpointService) Delete(id int) error {
	return database.GetDB().Delete(&model.WebhookEndpoint{}, id).Error
}

// Test sends a signed ping envelope and classifies failures into i18n keys.
func (s *EndpointService) Test(id int, source string) TestResult {
	row, err := s.Load(id)
	if err != nil {
		return TestResult{false, "load", "webhookErrorUnknown"}
	}
	return s.testEndpoint(row, source)
}

// TestEndpoint delivers a ping for an unsaved endpoint definition.
func (s *EndpointService) TestEndpoint(view EndpointView, secret string, source string) TestResult {
	if err := validateEndpoint(view, secret); err != nil {
		return TestResult{false, "url", classifyWebhookError(err)}
	}
	row := &model.WebhookEndpoint{
		Name:            view.Name,
		Url:             strings.TrimSpace(view.Url),
		Secret:          secret,
		AllowPrivate:    view.AllowPrivate,
		BackupTransport: normalizeTransport(view.BackupTransport),
	}
	return s.testEndpoint(row, source)
}

func (s *EndpointService) testEndpoint(row *model.WebhookEndpoint, source string) TestResult {
	env := NewEnvelope("webhook.test", source, time.Now(), map[string]string{
		"message": "3x-ui webhook test",
	})
	d, err := BuildJSONDelivery(env)
	if err != nil {
		return TestResult{false, "build", classifyWebhookError(err)}
	}
	if err := Send(row.Url, row.AllowPrivate, row.Secret, d); err != nil {
		return TestResult{false, "send", classifyWebhookError(err)}
	}
	return TestResult{true, "send", "webhookTestSuccess"}
}

func validateEndpoint(view EndpointView, secret string) error {
	if strings.TrimSpace(view.Name) == "" {
		return errors.New("webhook endpoint name is required")
	}
	raw := strings.TrimSpace(view.Url)
	if raw == "" {
		return errors.New("webhook endpoint URL is required")
	}
	if _, err := service.SanitizeHTTPURL(raw); err != nil {
		return err
	}
	if view.Enabled {
		if _, err := service.SanitizePublicHTTPURL(raw, view.AllowPrivate); err != nil {
			return err
		}
	}
	if view.BackupTransport != "" && normalizeTransport(view.BackupTransport) != TransportRaw && normalizeTransport(view.BackupTransport) != TransportJSONBase64 {
		return fmt.Errorf("unknown backupTransport %q", view.BackupTransport)
	}
	_ = secret
	return nil
}

func normalizeTransport(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return TransportRaw
	}
	return v
}

func normalizeEvents(v string) string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, ",")
}

// EventWanted reports whether the endpoint's filter accepts the event name.
func EventWanted(enabledEvents, event string) bool {
	if strings.TrimSpace(enabledEvents) == "" {
		return false
	}
	for e := range strings.SplitSeq(enabledEvents, ",") {
		if strings.TrimSpace(e) == event {
			return true
		}
	}
	return false
}
