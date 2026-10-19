package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSignBodyIsHMACSHA256(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"event":"xray.crash"}`)
	want := "sha256=" + hexHMAC(secret, body)
	if got := SignBody(secret, body); got != want {
		t.Fatalf("SignBody = %q, want %q", got, want)
	}
	if SignBody("", body) != "" {
		t.Fatal("empty secret must produce empty signature")
	}
}

func TestVerifySignatureRejectsWrongSecret(t *testing.T) {
	body := []byte("payload")
	sig := SignBody("right", body)
	if !VerifySignature("right", sig, body) {
		t.Fatal("signature with correct secret must verify")
	}
	if VerifySignature("wrong", sig, body) {
		t.Fatal("signature must not verify under a different secret")
	}
	if VerifySignature("right", sig, []byte("tampered")) {
		t.Fatal("signature must not verify for a modified body")
	}
}

func TestEnvelopeUsesOccurredAtNotDeliveryTime(t *testing.T) {
	occurred := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	env := NewEnvelope(EventLoginAttempt, "eu-1", occurred, LoginEventData{Username: "admin"})
	if env.OccurredAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("OccurredAt = %q, want 2026-01-01T00:00:00Z", env.OccurredAt)
	}
	if env.Source != "eu-1" || env.Event != EventLoginAttempt || env.Version != EnvelopeVersion {
		t.Fatalf("envelope identity wrong: %+v", env)
	}
	if env.Id == "" {
		t.Fatal("envelope id must be non-empty")
	}
}

func TestJSONDeliveryCarriesIdentityHeaders(t *testing.T) {
	env := NewEnvelope(EventOutboundDown, "eu-1", time.Unix(0, 0).UTC(), OutboundHealthData{Tag: "proxy-a", Error: "timeout"})
	d, err := BuildJSONDelivery(env)
	if err != nil {
		t.Fatal(err)
	}
	if d.ContentType != "application/json" {
		t.Fatalf("ContentType = %q", d.ContentType)
	}
	if d.Headers["X-Webhook-Event"] != EventOutboundDown {
		t.Fatalf("event header = %q", d.Headers["X-Webhook-Event"])
	}
	if d.Headers["X-Webhook-Timestamp"] != env.OccurredAt {
		t.Fatalf("timestamp header = %q", d.Headers["X-Webhook-Timestamp"])
	}
	var payload map[string]any
	if err := json.Unmarshal(d.Body, &payload); err != nil {
		t.Fatal(err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %#v", payload["data"])
	}
	if _, ok := data["tag"]; !ok {
		t.Fatalf("data must use camelCase json tags, got %v", data)
	}
	if _, ok := data["Tag"]; ok {
		t.Fatalf("data must not leak Go field names, got %v", data)
	}
}

func TestSendSignsBodyAndRefusesRedirects(t *testing.T) {
	var gotSig, gotEvent string
	var gotBody []byte
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Webhook-Signature")
		gotEvent = r.Header.Get("X-Webhook-Event")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// Redirecting front: must not be followed.
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landed", http.StatusFound)
	}))
	defer front.Close()

	env := NewEnvelope(EventCPUHigh, "s", time.Now(), SystemMetricData{Percent: 90, Threshold: 80})
	d, err := BuildJSONDelivery(env)
	if err != nil {
		t.Fatal(err)
	}
	err = Send(front.URL, true, "secret", d)
	if err == nil {
		t.Fatal("redirect must not be followed; Send should treat 302 as failure")
	}
	if gotSig != "" {
		t.Fatal("body must not reach the redirect target")
	}

	if err := Send(target.URL, true, "secret", d); err != nil {
		t.Fatalf("direct send: %v", err)
	}
	if gotEvent != EventCPUHigh {
		t.Fatalf("event header = %q", gotEvent)
	}
	wantSig := SignBody("secret", gotBody)
	if gotSig != wantSig {
		t.Fatalf("signature = %q, want %q", gotSig, wantSig)
	}
}

func TestSendBlocksPrivateTargetUnlessAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	env := NewEnvelope(EventXrayCrash, "s", time.Now(), CrashData{Detail: "boom"})
	d, err := BuildJSONDelivery(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := Send(srv.URL, false, "secret", d); err == nil {
		t.Fatal("loopback target must be rejected when allowPrivate=false")
	}
	if err := Send(srv.URL, true, "secret", d); err != nil {
		t.Fatalf("allowPrivate=true must accept loopback: %v", err)
	}
}

func TestRawFileDeliveryStreamsBytesAndMetadataHeaders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.db")
	payload := []byte("SQLite format 3\x00-rest-of-snapshot")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	env := NewEnvelope(EventBackupCompleted, "eu-1", time.Now(), BackupCompletedData{})
	d, err := BuildRawFileDelivery(env, path, "eu-1_2026.db")
	if err != nil {
		t.Fatal(err)
	}
	if d.ContentType != "application/octet-stream" {
		t.Fatalf("ContentType = %q", d.ContentType)
	}
	if d.Size != int64(len(payload)) {
		t.Fatalf("Size = %d, want %d", d.Size, len(payload))
	}
	sum := sha256.Sum256(payload)
	if d.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("SHA256 = %q", d.SHA256)
	}
	if d.Headers["X-Webhook-Filename"] != "eu-1_2026.db" {
		t.Fatalf("filename header = %q", d.Headers["X-Webhook-Filename"])
	}

	var gotBody []byte
	var gotLen int64
	var gotCT, gotFile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotLen = r.ContentLength
		gotCT = r.Header.Get("Content-Type")
		gotFile = r.Header.Get("X-Webhook-Filename")
		if r.Header.Get("X-Webhook-Signature") == "" {
			t.Error("raw delivery must still be signed")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := Send(srv.URL, true, "secret", d); err != nil {
		t.Fatal(err)
	}
	if gotCT != "application/octet-stream" {
		t.Fatalf("received Content-Type = %q", gotCT)
	}
	if string(gotBody) != string(payload) {
		t.Fatalf("body mismatch: %q", gotBody)
	}
	if gotLen != int64(len(payload)) {
		t.Fatalf("Content-Length = %d, want %d", gotLen, len(payload))
	}
	if gotFile != "eu-1_2026.db" {
		t.Fatalf("filename header = %q", gotFile)
	}
}

func TestBase64DeliveryEmbedsAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snap.db")
	payload := []byte("db-bytes")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	env := NewEnvelope(EventBackupCompleted, "s", time.Now(), BackupCompletedData{Filename: "snap.db", Size: 8})
	d, err := BuildBase64Delivery(env, path, "snap.db")
	if err != nil {
		t.Fatal(err)
	}
	var parsed Envelope
	if err := json.Unmarshal(d.Body, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(parsed.Attachments))
	}
	att := parsed.Attachments[0]
	if att.Name != "snap.db" || att.Size != 8 {
		t.Fatalf("attachment meta: %+v", att)
	}
	raw, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(payload) {
		t.Fatalf("decoded attachment = %q", raw)
	}
}

func TestClassifyWebhookError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, "webhookTestSuccess"},
		{errString(`Post "http://127.0.0.1:1/x": dial tcp 127.0.0.1:1: connection refused`), "webhookErrorConnRefused"},
		{errString(`context deadline exceeded`), "webhookErrorTimeout"},
		{errString(`x509: certificate signed by unknown authority`), "webhookErrorTLS"},
		{errString(`webhook returned status 500`), "webhookErrorBadStatus"},
		{errString(`blocked private/internal address 10.0.0.1`), "webhookErrorURL"},
		{errString(`something else`), "webhookErrorUnknown"},
	}
	for _, tc := range cases {
		if got := classifyWebhookError(tc.err); got != tc.want {
			t.Fatalf("classifyWebhookError(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestEventWantedFiltersCSV(t *testing.T) {
	if !EventWanted("login.attempt,cpu.high", "cpu.high") {
		t.Fatal("cpu.high must be wanted")
	}
	if EventWanted("login.attempt,cpu.high", "node.down") {
		t.Fatal("node.down must not be wanted")
	}
	if EventWanted("", "cpu.high") {
		t.Fatal("empty list must disable everything")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func hexHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
