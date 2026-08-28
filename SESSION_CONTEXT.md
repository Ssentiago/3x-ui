# Session context — Tariff override layer refactoring

## Architecture decision

**Read-only model**: tariff operations must NEVER write to existing DB columns/tables. All tariff override data lives in new columns on `client_tariffs` only.

### How it works

```
client_inbounds → ONLY client's own manually-attached inbounds (never touched by tariff)
client_tariffs   → one row per tariff membership per client
  ├─ total_gb_override, limit_ip_override, expiry_time_override → override VALUES
  ├─ inbound_ids_override (NEW, JSON array string) → override inbound IDs
  ├─ is_inbounds_overridden → boolean flag (already exists)
  └─ started_at, ended_at → membership period

ResolveClientFields → reads tariff chain, checks override flags/columns → effective values
```

## What's already fixed

### Backend
| File | What |
|------|------|
| `model.go:888` | `InboundsMode FieldMode` on `Client` struct |
| `client_crud.go:530-567` | REMOVED `ApplyInboundList` block + `inboundIds` filter from `Update` |
| `client_tariff.go:208-237` | `ReturnToTariff("inbounds")` simplified to just clear flag |
| `group.go:157-172` | REMOVED `ApplyInboundList` from group rename |
| `controller/group.go` | `returnToTariff` controller — added `clientService.DetachAllByEmail` for inbounds (WRONG, needs revert) |

### Frontend
| File | What |
|------|------|
| `TariffFormModal.tsx:100` | URL fix: `/panel/api/clients/tariffs/preview` |
| `TariffFormModal.tsx:104` | JSON headers added |
| `TariffFormModal.tsx:282-284` | Inbound names in chain (not "N inbounds") |
| `TariffFormModal.tsx:286` | Compact display for 10+ inbounds |
| `TariffFormModal.tsx:193` | Impact notice hidden in readonly mode |
| `GroupsPage.tsx` | URL-driven tabs (`/groups/tariffs`, `/groups/profiles`) |
| `routes.tsx` | Added alias routes for groups sub-pages |
| `AppSidebar.tsx` | `selectedKey` fix for `/groups/*` |
| `useClientTariffState.ts` | **NEW** hook — single source of truth for tariff state |
| `ClientFormModal.tsx` | Refactored to use new hook |
| `ClientFormModal.tsx:693` | `inboundsMode` sent in clientPayload |
| `ClientFormModal.tsx:253-315` | Transition effect: managed→local keeps displayed value, local→managed sets tariffValue |
| `ClientFormModal.tsx:826+` | `key={String(isFieldManaged(field))}` on inputs for force remount |

### useTariffOverrides.ts
- 404 fix: URL `/panel/api/client/get/effective/` → `/panel/api/clients/get/effective/`
- Data fix: `return res` → `return res.obj`
- Now unused in production (only tests)

### useClientTariffState.ts
- `staleTime: 0` + `removeQueries` on modal close (no stale cache)
- `hasTariff = !!selectedGroup?.tariff` (not `client?.group`)
- Exposes: `tariffValue`, `clientValue`, `isFieldManaged`, `makeLocal`, `returnToTariff`, `computeDiff`

### Tests fixed
| File | What |
|------|------|
| `useTariffOverrides.test.tsx` | Mock returns `{ success, msg, obj }` instead of `{}` |
| `client-form-tariff-detail.test.tsx` | URL `/client/get/effective/` → `/clients/get/effective/`, wrapped in Msg envelope |
| `client-form-resolve.test.tsx` | Replaced `mock.calls` check with UI assertion (countLocks) |

## What's NOT done — needs implementation

### 1. Add `inbound_ids_override` column to `ClientTariff` (model.go ~line 1385)
```go
InboundIDsOverride *string `json:"inboundIDsOverride" gorm:"column:inbound_ids_override"`
```
After `IsInboundsOverridden` field. GORM AutoMigrate handles migration.

### 2. Update `OverrideField("inbounds")` in client_tariff.go (~line 182)
Currently: `db.Model(&ct).Update("is_inbounds_overridden", true)`
Change to:
```go
case "inbounds":
    idsJSON, _ := json.Marshal(f.InboundIds)
    return db.Model(&ct).Updates(map[string]any{
        "is_inbounds_overridden": true,
        "inbound_ids_override":   string(idsJSON),
    }).Error
```
Add `"encoding/json"` import.

### 3. Update `ReturnToTariff("inbounds")` in client_tariff.go (~line 208)
Change to clear both flag and column:
```go
case "inbounds":
    return db.Model(&ct).Updates(map[string]any{
        "is_inbounds_overridden": false,
        "inbound_ids_override":   nil,
    }).Error
```

### 4. Update `ResolveClientFields` in client_resolve.go
Find where inbound IDs are resolved when `is_inbounds_overridden` is true.
Currently reads from `client_inbounds` table. Change to use `ct.InboundIDsOverride`:
- Parse JSON string with `json.Unmarshal`
- If overridden and override column is set, use those IDs
- Otherwise use tariff chain IDs (normal resolution)

### 5. Remove `ApplyInboundList` from group.go:282 (bulkAdd)
Remove the block that calls `cts.ApplyInboundList(...)`. Only `RefreshTrafficForGroup` should remain.

### 6. Revert `controller/group.go` returnToTariff
Remove the `DetachAllByEmail` call added earlier — it's no longer needed with the new column approach.
`DetachAllByEmail` function was never created anyway (build error).

### 7. Revert `GetOwnInboundIdsForRecord` in client_get.go
If the build agent added `GetOwnInboundIdsForRecord` (filtering tariff inbounds from `client_inbounds`), revert to plain `GetInboundIdsForRecord`. With new architecture, `client_inbounds` is always own-only — no filter needed.

### 8. buildClientPayload in controller/client.go
Revert any changes that use `GetOwnInboundIdsForRecord` — should use `GetInboundIdsForRecord` directly.

## Key files and line numbers
| File | Key lines |
|------|-----------|
| `internal/database/model/model.go` | 1376-1386 (ClientTariff), 867-896 (Client), 1422-1427 (ResolvedFields) |
| `internal/web/service/client_tariff.go` | 104-160 (ApplyInboundList), 162-187 (OverrideField), 189-237 (ReturnToTariff) |
| `internal/web/service/client_resolve.go` | 213-230 (ClientEffective), 544-578 (GetEffective), 92-152 (ResolveChainPreview) |
| `internal/web/service/client_crud.go` | 313-577 (Update function) |
| `internal/web/service/client_get.go` | 124-165 (GetInboundIdsForRecord/GetOwnInboundIdsForRecord) |
| `internal/web/controller/group.go` | 157-172 (rename ApplyInboundList), 266-284 (bulkAdd ApplyInboundList), 444-462 (returnToTariff) |
| `internal/web/controller/client.go` | 111-115 (buildClientPayload), 233-241 (update) |
| `frontend/src/hooks/useClientTariffState.ts` | NEW HOOK |
| `frontend/src/pages/clients/ClientFormModal.tsx` | 236-315 (tariff state + effects), 667-693 (clientPayload), 826-903 (inputs with key) |
| `frontend/src/pages/groups/TariffFormModal.tsx` | 97-108 (preview query), 278-289 (chain labels) |

## Verification commands
```bash
go build ./internal/...
cd frontend && npm run typecheck
```
