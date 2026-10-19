package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const (
	jsonSendTimeout = 10 * time.Second
	rawSendTimeout  = 5 * time.Minute
)

// Delivery is the signed HTTP POST a receiver gets for one event.
type Delivery struct {
	ContentType string
	Body        []byte
	// FilePath, when set, replaces Body as the request entity and is streamed
	// from disk with Content-Length set from the file size.
	FilePath string
	Filename string
	Size     int64
	SHA256   string
	Headers  map[string]string
}

// Signer signs request bodies with HMAC-SHA256. Empty secret yields an empty
// signature header value, so receivers can require a signature explicitly.
func SignBody(secret string, body []byte) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SignReader signs a stream without buffering it (used for raw file bodies).
func SignReader(secret string, r io.Reader) (string, error) {
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := io.Copy(mac, r); err != nil {
		return "", err
	}
	return "sha256=" + hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifySignature reports whether sig matches body under secret. Comparison
// is constant-time.
func VerifySignature(secret, sig string, body []byte) bool {
	want := SignBody(secret, body)
	if want == "" || sig == "" {
		return false
	}
	return hmac.Equal([]byte(want), []byte(sig))
}

// BuildJSONDelivery encodes the envelope as application/json.
func BuildJSONDelivery(env Envelope) (*Delivery, error) {
	body, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	d := &Delivery{
		ContentType: "application/json",
		Body:        body,
		Headers: map[string]string{
			"X-Webhook-Event":     env.Event,
			"X-Webhook-Id":        env.Id,
			"X-Webhook-Source":    env.Source,
			"X-Webhook-Timestamp": env.OccurredAt,
		},
	}
	return d, nil
}

// BuildRawFileDelivery streams path as application/octet-stream. Metadata that
// would live in the JSON envelope rides in headers so a receiver can route and
// authenticate before parsing the body.
func BuildRawFileDelivery(env Envelope, path, filename string) (*Delivery, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	_, copyErr := io.Copy(h, f)
	f.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	digest := hex.EncodeToString(h.Sum(nil))

	d := &Delivery{
		ContentType: "application/octet-stream",
		FilePath:    path,
		Filename:    filename,
		Size:        info.Size(),
		SHA256:      digest,
		Headers: map[string]string{
			"X-Webhook-Event":     env.Event,
			"X-Webhook-Id":        env.Id,
			"X-Webhook-Source":    env.Source,
			"X-Webhook-Timestamp": env.OccurredAt,
			"X-Webhook-Filename":  filename,
			"X-Webhook-Size":      fmt.Sprintf("%d", info.Size()),
			"X-Webhook-SHA256":    digest,
		},
	}
	return d, nil
}

// BuildBase64Delivery wraps the file as an Envelope attachment for
// JSON-only receivers.
func BuildBase64Delivery(env Envelope, path, filename string) (*Delivery, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	env.Attachments = []Attachment{{
		Name:        filename,
		ContentType: "application/vnd.sqlite3",
		Size:        int64(len(raw)),
		SHA256:      hex.EncodeToString(sum[:]),
		Data:        base64.StdEncoding.EncodeToString(raw),
	}}
	if meta, ok := env.Data.(BackupCompletedData); ok {
		meta.Truncated = false
		env.Data = meta
	}
	return BuildJSONDelivery(env)
}

// Send posts the delivery to url. It re-checks the URL against the SSRF guard
// at request time and refuses redirects, so a hostname that passed validation
// at save time cannot 302 into the panel host's private network.
func Send(url string, allowPrivate bool, secret string, d *Delivery) error {
	safeURL, err := service.SanitizePublicHTTPURL(url, allowPrivate)
	if err != nil {
		return fmt.Errorf("webhook url: %w", err)
	}

	var body io.Reader
	var contentLength int64
	var getBody func() (io.ReadCloser, error)
	if d.FilePath != "" {
		f, err := os.Open(d.FilePath)
		if err != nil {
			return err
		}
		body = f
		contentLength = d.Size
		getBody = func() (io.ReadCloser, error) { return os.Open(d.FilePath) }
		sig, err := SignReader(secret, f)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return err
		}
		d.Headers["X-Webhook-Signature"] = sig
	} else {
		d.Headers["X-Webhook-Signature"] = SignBody(secret, d.Body)
		body = bytes.NewReader(d.Body)
		contentLength = int64(len(d.Body))
	}

	timeout := jsonSendTimeout
	if d.FilePath != "" {
		timeout = rawSendTimeout
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, safeURL, body)
	if err != nil {
		if d.FilePath != "" {
			if closer, ok := body.(io.Closer); ok {
				closer.Close()
			}
		}
		return err
	}
	if d.FilePath != "" {
		if closer, ok := body.(io.Closer); ok {
			defer closer.Close()
		}
		req.GetBody = getBody
	}
	req.ContentLength = contentLength
	req.Header.Set("Content-Type", d.ContentType)
	req.Header.Set("User-Agent", "3x-ui-webhook/1.0")
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// classifyWebhookError maps a delivery error to an i18n key under
// "pages.settings." — same contract as classifySMTPError.
func classifyWebhookError(err error) string {
	if err == nil {
		return "webhookTestSuccess"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "dial"):
		return "webhookErrorConnRefused"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return "webhookErrorTimeout"
	case strings.Contains(msg, "tls") || strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return "webhookErrorTLS"
	case strings.Contains(msg, "webhook returned status"):
		return "webhookErrorBadStatus"
	case strings.Contains(msg, "blocked private") || strings.Contains(msg, "webhook url"):
		return "webhookErrorURL"
	default:
		return "webhookErrorUnknown"
	}
}
