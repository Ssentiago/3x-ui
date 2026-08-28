# Reality SNI Overhaul — Implementation Plan

## Summary

Валидация Reality SNI таргетов при добавлении. Панель проверяет что таргет поддерживает TLS 1.3 + H2 — обязательные требования Reality протокола.

**Key design principle:** Protocol check (TLS 1.3 + H2), не reachability check. Split-brain не влияет.

---

## 1. Validation Logic

### 1.1 Validation Function

**New file:** `internal/web/service/reality_probe.go`

```go
// ValidationResult carries results of a protocol check.
type ValidationResult struct {
    Ok         bool          // TLS 1.3 + H2 supported
    TLS13      bool          // TLS 1.3 negotiated
    H2         bool          // HTTP/2 ALPN negotiated
    CertValid  bool          // cert not expired, chain valid
    CertExpiry time.Time     // when the cert expires
    Error      string        // error message if check failed
}

// ValidateRealityTarget checks if a target supports TLS 1.3 + H2.
func ValidateRealityTarget(target, sni string, timeout time.Duration) *ValidationResult
```

Implementation:
1. `net.DialTimeout("tcp", target, timeout)` — basic reachability (for timeout only)
2. `tls.DialWithDialer` with `MinVersion: tls.VersionTLS13`, `NextProtos: ["h2", "http/1.1"]`, `ServerName: sni`
3. Check `conn.ConnectionState().NegotiatedProtocol == "h2"`
4. Verify `conn.ConnectionState().PeerCertificates[0]` is not expired and chain is valid
5. Return `ValidationResult` with per-check results

---

## 2. API Endpoint

### 2.1 Server Controller

**File:** `internal/web/controller/server.go`

```go
g.POST("/validateRealityTarget", a.validateRealityTarget)
```

### 2.2 Handler

```go
func (a *ServerController) validateRealityTarget(c *gin.Context) {
    var req struct {
        Target string `json:"target" binding:"required"` // "www.amazon.com:443"
        Sni    string `json:"sni" binding:"required"`    // "www.amazon.com"
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        jsonMsg(c, "invalid request", err)
        return
    }

    timeout := 5 * time.Second
    result := service.ValidateRealityTarget(req.Target, req.Sni, timeout)
    jsonObj(c, result, nil)
}
```

---

## 3. Frontend

### 3.1 Validation Button

**File:** `frontend/src/pages/inbounds/form/security/reality.tsx`

Next to the existing Reality target/SNI input fields, add a "Validate" button:

```
┌─ Reality ───────────────────────────────────────┐
│ Target: [www.amazon.com:443        ] [Random]    │
│ SNI:    [www.amazon.com            ] [Random]    │
│                                        [Validate]│
│                                                    │
│ ✅ Valid: TLS 1.3 ✓  H2 ✓  Cert valid (365d)     │
└──────────────────────────────────────────────────┘
```

Or error state:
```
│ ❌ Invalid: TLS 1.3 not supported                 │
```

### 3.2 API Call

```ts
const validateTarget = async (target: string, sni: string) => {
  const msg = await HttpUtil.post('/panel/api/server/validateRealityTarget', { target, sni });
  if (msg?.success) {
    const result = msg.obj as ValidationResult;
    // show result in UI
  }
};
```

---

## 4. File Change Summary

### New Files (1)
| File | Purpose |
|------|---------|
| `internal/web/service/reality_probe.go` | TLS 1.3 + H2 validation function |

### Modified Files (2)
| File | Changes |
|------|---------|
| `internal/web/controller/server.go` | Add `POST /validateRealityTarget` endpoint |
| `frontend/src/pages/inbounds/form/security/reality.tsx` | Add Validate button + result display |

---

## 5. Implementation Order

1. **Validation function** — `reality_probe.go` with TLS 1.3 + H2 check
2. **API endpoint** — `POST /validateRealityTarget` in server controller
3. **Frontend** — Validate button + result display
4. **Testing** — Unit tests + manual E2E

---

## 6. Edge Cases

### 6.1 Protocol Check vs Reachability
Validation checks TLS 1.3 + H2 support — this is a protocol requirement for Reality. Split-brain doesn't affect protocol checks: if the target supports TLS 1.3 + H2, it will work for Reality (assuming client can reach it).

### 6.2 Certificate Check
Validation checks:
- `x509.Certificate.NotAfter > time.Now()` — cert not expired
- Chain verification via system roots — not self-signed

### 6.3 Timeout
Default timeout: 5s. If target is unreachable, validation will timeout and report error. This is expected — admin can retry or choose different target.

---

## 7. Testing Strategy

### Unit Tests
- `reality_probe_test.go`: Mock TLS connections, test validation logic

### Manual E2E
1. Open Reality inbound form
2. Enter target: "www.amazon.com:443", SNI: "www.amazon.com"
3. Click "Validate"
4. Verify: ✅ Valid: TLS 1.3 ✓ H2 ✓ Cert valid
5. Enter invalid target (no TLS 1.3)
6. Click "Validate"
7. Verify: ❌ Invalid: TLS 1.3 not supported
