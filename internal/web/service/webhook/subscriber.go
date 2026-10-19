package webhook

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// SettingReader is the subset of SettingService the webhook channel needs.
type SettingReader interface {
	GetWebhookCpu() (int, error)
	GetWebhookMemory() (int, error)
	GetWebhookSourceName() (string, error)
}

// Subscriber fans eventbus events out to every enabled webhook endpoint.
type Subscriber struct {
	settings  SettingReader
	endpoints *EndpointService
	limiter   *eventbus.RateLimiter
}

func NewSubscriber(settings SettingReader) *Subscriber {
	return &Subscriber{
		settings:  settings,
		endpoints: &EndpointService{},
		limiter:   eventbus.NewRateLimiter(1 * time.Minute),
	}
}

// HandleEvent is the eventbus subscriber callback.
func (s *Subscriber) HandleEvent(e eventbus.Event) {
	targets, err := s.endpoints.EnabledTargets()
	if err != nil || len(targets) == 0 {
		return
	}
	eventName := string(e.Type)
	if e.Type != eventbus.EventLoginAttempt {
		if !s.limiter.Allow(e.Type, e.Source) {
			return
		}
	}
	data, ok := wireData(s.settings, e)
	if !ok {
		return
	}
	source, _ := s.settings.GetWebhookSourceName()
	env := NewEnvelope(eventName, source, e.Timestamp, data)
	for _, t := range targets {
		if !EventWanted(t.EnabledEvents, eventName) {
			continue
		}
		s.deliver(t, env)
	}
}

func (s *Subscriber) deliver(t *model.WebhookEndpoint, env Envelope) {
	d, err := BuildJSONDelivery(env)
	if err != nil {
		logger.Warning("webhook subscriber: build failed:", err)
		return
	}
	if err := Send(t.Url, t.AllowPrivate, t.Secret, d); err != nil {
		logger.Warning("webhook subscriber: send failed:", err)
	}
}

// wireData maps an eventbus payload onto the stable DTO the channel owns.
// Returns false when the event should be dropped (below-threshold metrics).
func wireData(settings SettingReader, e eventbus.Event) (any, bool) {
	switch e.Type {
	case eventbus.EventOutboundDown, eventbus.EventOutboundUp:
		d := OutboundHealthData{Tag: e.Source}
		if hd, ok := e.Data.(*eventbus.OutboundHealthData); ok {
			d.Delay = hd.Delay
			d.Error = hd.Error
		}
		return d, true
	case eventbus.EventNodeDown, eventbus.EventNodeUp:
		d := NodeHealthData{}
		if hd, ok := e.Data.(*eventbus.NodeHealthData); ok {
			d.NodeId = hd.NodeId
			d.LatencyMs = hd.LatencyMs
			d.CpuPct = hd.CpuPct
			d.MemPct = hd.MemPct
			d.XrayState = hd.XrayState
			d.XrayError = hd.XrayError
		}
		return d, true
	case eventbus.EventXrayCrash:
		d := CrashData{}
		if e.Data != nil {
			d.Detail = fmt.Sprint(e.Data)
		}
		return d, true
	case eventbus.EventLoginAttempt:
		d := LoginEventData{Username: e.Source, Status: "fail"}
		if ld, ok := e.Data.(*eventbus.LoginEventData); ok {
			d.Username = ld.Username
			d.IP = ld.IP
			d.Time = ld.Time
			d.Status = ld.Status
			d.Reason = ld.Reason
		}
		return d, true
	case eventbus.EventCPUHigh:
		sm, ok := e.Data.(*eventbus.SystemMetricData)
		if !ok {
			return nil, false
		}
		threshold, err := settings.GetWebhookCpu()
		if err != nil || threshold <= 0 || sm.Percent <= float64(threshold) {
			return nil, false
		}
		return SystemMetricData{Percent: sm.Percent, Threshold: threshold}, true
	case eventbus.EventMemoryHigh:
		sm, ok := e.Data.(*eventbus.SystemMetricData)
		if !ok {
			return nil, false
		}
		threshold, err := settings.GetWebhookMemory()
		if err != nil || threshold <= 0 || sm.Percent <= float64(threshold) {
			return nil, false
		}
		return SystemMetricData{Percent: sm.Percent, Threshold: threshold}, true
	default:
		return nil, false
	}
}

// WantsCPU / WantsMemory mirror the channel-level gate the cron jobs use.
func WantsCPU(settings SettingReader, targets []*model.WebhookEndpoint) bool {
	threshold, err := settings.GetWebhookCpu()
	if err != nil || threshold <= 0 {
		return false
	}
	return anyWants(targets, EventCPUHigh)
}

func WantsMemory(settings SettingReader, targets []*model.WebhookEndpoint) bool {
	threshold, err := settings.GetWebhookMemory()
	if err != nil || threshold <= 0 {
		return false
	}
	return anyWants(targets, EventMemoryHigh)
}

func anyWants(targets []*model.WebhookEndpoint, event string) bool {
	for _, t := range targets {
		if EventWanted(t.EnabledEvents, event) {
			return true
		}
	}
	return false
}

// BackupMeta describes one snapshot about to be delivered.
type BackupMeta struct {
	Filename string
	FilePath string
	Size     int64
	SHA256   string
}

// Notifier delivers panel-originated events (backup) to webhook endpoints.
type Notifier struct {
	endpoints *EndpointService
	settings  SettingReader
}

func NewNotifier(settings SettingReader) *Notifier {
	return &Notifier{endpoints: &EndpointService{}, settings: settings}
}

// NotifyBackupCompleted fans a snapshot out according to each endpoint's
// transport preference. Fire-and-forget: failures are logged only.
func (n *Notifier) NotifyBackupCompleted(meta BackupMeta) {
	targets, err := n.endpoints.EnabledTargets()
	if err != nil {
		return
	}
	source, _ := n.settings.GetWebhookSourceName()
	env := NewEnvelope(EventBackupCompleted, source, time.Now(), BackupCompletedData{
		Filename: meta.Filename,
		Size:     meta.Size,
		SHA256:   meta.SHA256,
	})
	for _, t := range targets {
		if !EventWanted(t.EnabledEvents, EventBackupCompleted) {
			continue
		}
		n.deliverBackup(t, env, meta)
	}
}

// NotifyBackupFailed sends a JSON error envelope for a failed snapshot.
func (n *Notifier) NotifyBackupFailed(err error) {
	targets, tErr := n.endpoints.EnabledTargets()
	if tErr != nil {
		return
	}
	source, _ := n.settings.GetWebhookSourceName()
	msg := "backup failed"
	if err != nil {
		msg = err.Error()
	}
	env := NewEnvelope(EventBackupFailed, source, time.Now(), BackupFailedData{Error: msg})
	for _, t := range targets {
		if !EventWanted(t.EnabledEvents, EventBackupFailed) {
			continue
		}
		d, dErr := BuildJSONDelivery(env)
		if dErr != nil {
			continue
		}
		if sErr := Send(t.Url, t.AllowPrivate, t.Secret, d); sErr != nil {
			logger.Warning("webhook backup-failed notify:", sErr)
		}
	}
}

func (n *Notifier) deliverBackup(t *model.WebhookEndpoint, env Envelope, meta BackupMeta) {
	var d *Delivery
	var err error
	switch t.BackupTransport {
	case TransportJSONBase64:
		d, err = BuildBase64Delivery(env, meta.FilePath, meta.Filename)
	default:
		if !t.BackupAttachFile {
			env.Data = BackupCompletedData{
				Filename:  meta.Filename,
				Size:      meta.Size,
				SHA256:    meta.SHA256,
				Truncated: true,
			}
			d, err = BuildJSONDelivery(env)
		} else {
			d, err = BuildRawFileDelivery(env, meta.FilePath, meta.Filename)
		}
	}
	if err != nil {
		logger.Warning("webhook backup build:", err)
		return
	}
	if err := Send(t.Url, t.AllowPrivate, t.Secret, d); err != nil {
		logger.Warning("webhook backup send:", err)
	}
}

// SourceName falls back to the OS hostname so multi-panel collectors can tell
// senders apart without extra configuration.
func SourceName(settings SettingReader) string {
	name, err := settings.GetWebhookSourceName()
	if err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "panel"
}
