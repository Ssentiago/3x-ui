package webhook

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Envelope version. Bump when the wire shape changes incompatibly.
const EnvelopeVersion = 1

// Event names accepted in WebhookEndpoint.EnabledEvents and sent in
// X-Webhook-Event. The first block mirrors internal/eventbus; backup events
// are panel-originated and never ride the bus (file payloads are too large).
const (
	EventOutboundDown    = "outbound.down"
	EventOutboundUp      = "outbound.up"
	EventXrayCrash       = "xray.crash"
	EventNodeDown        = "node.down"
	EventNodeUp          = "node.up"
	EventCPUHigh         = "cpu.high"
	EventMemoryHigh      = "memory.high"
	EventLoginAttempt    = "login.attempt"
	EventBackupCompleted = "db.backup.completed"
	EventBackupFailed    = "db.backup.failed"
)

// AllEvents is the full filter vocabulary for EnabledEvents CSV lists.
var AllEvents = []string{
	EventOutboundDown,
	EventOutboundUp,
	EventXrayCrash,
	EventNodeDown,
	EventNodeUp,
	EventCPUHigh,
	EventMemoryHigh,
	EventLoginAttempt,
	EventBackupCompleted,
	EventBackupFailed,
}

// BackupTransport values stored on WebhookEndpoint.BackupTransport.
const (
	TransportRaw        = "raw"
	TransportJSONBase64 = "json_base64"
)

// Envelope is the public JSON contract for every webhook event body.
// Data is one of the *Data structs below — never a raw eventbus payload.
type Envelope struct {
	Version     int          `json:"version"`
	Id          string       `json:"id"`
	Event       string       `json:"event"`
	Source      string       `json:"source"`
	OccurredAt  string       `json:"occurredAt"`
	Data        any          `json:"data"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment carries a file inside a json_base64 backup delivery.
type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Data        string `json:"data"`
}

// OutboundHealthData is the wire shape of outbound.down / outbound.up.
type OutboundHealthData struct {
	Tag   string `json:"tag"`
	Delay int64  `json:"delay"`
	Error string `json:"error,omitempty"`
}

// NodeHealthData is the wire shape of node.down / node.up.
type NodeHealthData struct {
	NodeId    int     `json:"nodeId"`
	LatencyMs int     `json:"latencyMs"`
	CpuPct    float64 `json:"cpuPct"`
	MemPct    float64 `json:"memPct"`
	XrayState string  `json:"xrayState"`
	XrayError string  `json:"xrayError,omitempty"`
}

// LoginEventData is the wire shape of login.attempt.
type LoginEventData struct {
	Username string `json:"username"`
	IP       string `json:"ip"`
	Time     string `json:"time,omitempty"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

// SystemMetricData is the wire shape of cpu.high / memory.high.
type SystemMetricData struct {
	Percent   float64 `json:"percent"`
	Threshold int     `json:"threshold"`
}

// CrashData is the wire shape of xray.crash.
type CrashData struct {
	Detail string `json:"detail,omitempty"`
}

// BackupCompletedData is the wire shape of db.backup.completed metadata.
type BackupCompletedData struct {
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Truncated bool   `json:"truncated"`
}

// BackupFailedData is the wire shape of db.backup.failed.
type BackupFailedData struct {
	Error string `json:"error"`
}

// NewEnvelope stamps identity and occurrence time. occurredAt is the moment
// the event was detected (e.Timestamp), not when delivery got around to it.
func NewEnvelope(event, source string, occurredAt time.Time, data any) Envelope {
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	return Envelope{
		Version:    EnvelopeVersion,
		Id:         NewEventId(),
		Event:      event,
		Source:     source,
		OccurredAt: occurredAt.UTC().Format(time.RFC3339),
		Data:       data,
	}
}

// NewEventId returns a random 128-bit hex id for X-Webhook-Id / Envelope.Id.
func NewEventId() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}
