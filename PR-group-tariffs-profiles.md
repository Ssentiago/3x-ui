## Summary

Introduces a three-entity **Group / Tariff / Profile** model to 3x-ui. A **Profile** is a reusable brick of concrete field values (traffic GB, expiry days, IP limit, inbound access); a **Tariff** composes an ordered chain of Profiles plus per-field merge strategies (`overwrite`/`sum` for traffic, `overwrite`/`union` for inbounds); a **Group** can be bound to one Tariff. Clients in a tariff-bound group automatically receive tariff-resolved traffic quota, expiry, IP limit and inbound assignments, computed at read time — so editing a tariff or its profile chain takes effect for every affected group's clients immediately, without touching each client. Any managed field can be per-client overridden (or returned to tariff control) from the client edit form.

## Why

Closes #5900, closes #5371, closes #5026.

The old system had client groups only as a free-text label on the client row — there was no structured way to define traffic/expiry/IP/inbound policy per group. Admins had to manually set `totalGB`, `expiryTime`, `limitIp` and inbound attachments on every client, and group-wide changes meant bulk-editing clients one by one.

### Mental model

The three entities form a pyramid — Profiles at the bottom, Tariffs in the middle, Groups at the top:

```
Group "VIP"  ────►  Tariff "Gold"
                       │
                       ├── Profile "Base"     (traffic 50GB, expiry 30d)
                       ├── Profile "Extra"    (traffic 100GB, limitIP 3)
                       └── Profile "Region"   (inbounds [US-East, EU-West])
```

**Profile** — a named bag of field values. Each value is optional (`NULL` means "I don't set this field"). Profiles are reusable across tariffs. Example: a "Base" profile with 50 GB traffic and 30-day expiry could be the starting point for both "Gold" and "Silver" tariffs.

**Tariff** — an ordered chain of Profiles + how to merge them. The tariff has no values of its own — it only says *which* profiles to apply, *in what order*, and *how* to combine overlapping fields. One tariff can serve many groups; changing the tariff instantly updates every group's clients.

**Group** — a named set of clients. A group can be bound to at most one tariff. Clients in a tariff-bound group get their traffic/expiry/IP/inbounds from the tariff, unless the admin overrides a specific field on a specific client.

### Resolution strategies

When reading a client, the system walks the tariff's profile chain in order and applies per-field strategies:

**Traffic** — `overwrite` (default) or `sum`:
- `overwrite`: the **last** profile that sets traffic wins. Profile A: 50 GB → Profile B: 100 GB → effective = 100 GB.
- `sum`: **add** every profile's traffic together. Profile A: 50 GB → Profile B: 30 GB → effective = 80 GB.

**Expiry days** — always last-wins (no strategy selector):
- Profile A: 30d → Profile B: 7d → effective = 7d.
- Expiry is anchored to the `started_at` timestamp on the `client_tariffs` row (set when the client enters the group) to avoid drift: `expiryTime = startedAt + (expiryDays × 86400s)`.

**IP limit** — always last-wins (no strategy selector):
- Profile A: 3 IPs → Profile B: 5 IPs → effective = 5 IPs.

**Inbounds** — `overwrite` (default) or `union`:
- `overwrite`: the **last** profile's inbound set replaces all prior ones. Profile A: [1,2] → Profile B: [3] → effective = [3].
- `union`: **deduplicated merge** of all profiles' inbound sets. Profile A: [1,2] → Profile B: [2,3] → effective = [1,2,3].

`NULL` values are always **skipped** — a profile that sets `limitIP: null` does not zero out the limit, it simply declines to participate in that field. To explicitly set "unlimited", use `0`.

### Override system

Any tariff-managed field can be **overridden** per-client. The client edit form shows locked fields with a lock icon and a "make local" button. Once overridden, the field stops following the tariff — the client's value is frozen until the admin clicks "return to tariff". Override priority: `override > tariff chain > client default`.

This is stored on the active `client_tariffs` row for the client (`total_gb_override`, `limit_ip_override`, `expiry_time_override` — all nullable). For inbounds, an `is_inbounds_overridden` flag controls whether the resolver uses the client's own `client_inbounds` entries (override active) or the tariff-resolved chain (override inactive). The `client_inbounds` join table is never rewritten for tariff purposes — it stays as the client's own inbound assignments. When a client leaves a tariff (the `client_tariffs` row is ended), all overrides are automatically cleaned up.

### Read-time resolution

All four fields (traffic, expiry, IP limit, inbounds) are resolved **on the fly** at read time — no client data is mutated in the database.

- **Traffic / Expiry / IP limit:** `ResolveClientFields` → `resolveChain` (strategy-aware overlay) → override check on the active `client_tariffs` row → `ToClientEffective`. The paged client list uses equivalent SQL expressions (`sqlEffTotalGB`, `sqlEffExpiry`, `sqlEffLimitIP`).
- **Inbounds:** `resolveEffectiveInboundIds` walks the tariff's profile chain, applies `overwrite`/`union` strategy, and returns the effective inbound set. Used by the client card (`GetInboundIdsForRecord`), Xray config (`ListForInbound`), subscription (`ListForInboundBySubId`), and the client list (`resolveEffectiveInboundsForPage` batch-resolves per page with a pre-computed tariff chain cache). When a group is unbound from a tariff, every client's own inbounds are instantly restored — nothing was lost because nothing was mutated.

This means:
- Editing a tariff or its profile chain takes effect for every client in every bound group **immediately**, without touching individual client rows.
- When a client moves between groups (or a group's tariff changes), a `client_tariffs` row is created with `started_at = now` for clients that don't yet have an active row, so expiry stays anchored to first membership.
- Unbinding a tariff from a group instantly restores every client's own per-field values and inbound assignments.
- When an inbound list changes through a tariff edit, xray is restarted automatically to apply the new inbound attachments.

The TariffFormModal uses a server-side preview via `POST /panel/api/tariffs/preview` (`ResolveChainPreview` in `client_resolve.go`) which returns resolved effective values with source profile names — so the admin sees the resolved effective values before saving.

## Type of change
- [ ] Bug fix
- [x] New feature
- [x] Refactoring (no behavior change)
- [ ] Documentation
- [ ] Tests only
- [ ] Build / CI / tooling
- [ ] Other

## Areas affected
- [x] Frontend (UI / panel pages)
- [x] Backend (API endpoints, login, settings)
- [x] Xray config generation
- [x] Subscription (share links / Clash / JSON)
- [ ] Statistics / traffic counters
- [x] Database / migrations
- [ ] Install / upgrade script
- [ ] Docker image
- [ ] Multi-node (sub-nodes)
- [ ] Telegram bot

### Backend

**Models / DB** — `internal/database/model/`:
- New `Tariff` (table `tariffs`), `Profile` + `TariffProfile` + `TariffProfileItem` + `ResolvedFields` + strategy constants (table `profiles`, `tariff_profiles`) in `internal/database/model/profile.go` and `model.go`.
- `ClientGroup` gains `TariffID *int` (`client_groups.tariff_id`).
- New `ClientTariff` model / table `client_tariffs` (`id`, `client_id`, `tariff_id`, `started_at`, `ended_at`) — append-only history of a client's tariff memberships. Also holds the four override columns: `total_gb_override` (nullable int64), `limit_ip_override` (nullable int), `expiry_time_override` (nullable int64), `is_inbounds_overridden` (bool). `ClientRecord` has no override columns.

**Migrations** — `internal/database/db.go` + `internal/database/migrate_data.go`: `Tariff`, `Profile`, `TariffProfile`, `ClientTariff` registered in both `allModels()` and `migrationModels()`. All changes are additive via GORM `AutoMigrate`: four new tables, new column on `client_groups`, nothing deleted or altered.

**API endpoints** — `internal/web/controller/group.go`, `profile.go`, `client.go` (all mounted under `/panel/api/clients`):
- Groups: `GET /groups`, `GET /groups/:name/emails`, `POST /groups/create` (now accepts `tariffId`), `POST /groups/rename` (creates `client_tariffs` rows for clients entering the new tariff), `POST /groups/delete`, `POST /groups/resetTariff`, `POST /groups/resetTraffic`, `POST /groups/bulkAdd` (creates `client_tariffs` rows for tariff-bound groups), `POST /groups/bulkRemove`.
- Tariffs: `GET /tariffs`, `GET /tariffs/:id` (returns `profiles` chain + `resolved` fields + group/client counts), `POST /tariffs/create`, `POST /tariffs/:id/update`, `POST /tariffs/:id/delete` (refused while any group references it), `POST /tariffs/:id/profiles` (atomic ordered-chain replace), `POST /tariffs/preview` (chain preview without save — returns `ResolvedFields` with source profile names).
- Profiles: `GET /profiles`, `GET /profiles/:id`, `POST /profiles/create`, `POST /profiles/:id/update`, `POST /profiles/:id/delete` (refused while any tariff uses it).
- Clients: new `GET /get/resolve/:email?group=<name>` ("what-if" preview of tariff-resolved values for a candidate group), `GET /get/effective/:email` (full tariff-resolved client), `POST /overrideField` and `POST /returnToTariff` (freeze / release a managed field for one client on the `client_tariffs` row; `inbounds` return triggers Attach/Detach via gRPC + xray restart).

**Business logic** — `internal/web/service/`:
- `tariff.go` — `TariffService` CRUD, chain resolution (`resolveTariff`), group/client counts, and traffic refresh (`RefreshTrafficForGroup*`, `refreshTariffTraffic`) that re-writes `client_traffics.total/expiry_time` from effective values when a tariff changes.
- `client_resolve.go` — the core resolver: `resolveChain` (strategy-aware overlay), `ResolveClientFields` (single entry point: group→tariff→chain→override check on `client_tariffs` row), `ResolveClientLimits`, `ResolveChainPreview` (server-side preview for `POST /tariffs/preview`), `ClientBatchResolver`, `ToClientEffective`.
- `client_tariff.go` — override API: `OverrideField` writes the effective value into the active `client_tariffs` row; `ReturnToTariff` clears it; `ApplyInboundList` performs Attach/Detach via gRPC on inbound return; `activateClientTariffsByEmails` manages `client_tariffs` rows (ends the prior row, creates a fresh one — old overrides are auto-cleared).
- `client_groups.go` — `ListGroups` now returns `tariffId`, `tariffName`, `tariff` summary; `CreateGroup(name, tariffId)`; `AddToGroup` calls `activateClientTariffsByEmails` when the target group is tariff-bound (closes the prior `client_tariffs` row and creates a fresh one — old overrides are cleaned up because they live on the closed row). All group functions return bare `error`.
- `client_paging.go` — `ClientSlim` carries `tariffName`, override values and `*IsOverridden` flags; SQL-level effective expressions (`sqlEffTotalGB`, `sqlEffExpiry`, `sqlEffLimitIP`) power depletion/near-depletion/expiry-range filtering and remaining/expiry sorting; `loadGroupTariffs` joins group→tariff; `resolveEffectiveInboundsForPage` batch-resolves effective inbound IDs per page with a pre-computed per-tariff chain cache.
- `client_link.go` — `ListForInbound` and `ListForInboundBySubId` resolve inbounds on-the-fly: direct `client_inbounds` rows for non-tariff/overridden clients + tariff-resolved clients (via `tariffIdsContainingInbound` which pre-filters tariffs whose chain includes the given inbound ID). Both emit effective values via `ToClientEffective`.
- `client_get.go` — `resolveEffectiveInboundIds` resolves a single client's effective inbounds through the tariff chain (applies `overwrite`/`union`). `GetInboundIdsForRecord`, `GetInboundIdsForEmail`, and `List()` all use it.
- `inbound_traffic.go` — `AddClientStat`/`UpdateClientStat` write effective `total`/`expiry_time` into `client_traffics` via `resolveEffectiveTraffic`.
- `profile.go` — `ProfileService` CRUD with validation and tariff-usage guards.
- `internal/web/job/check_client_ip_job.go` — fail2ban IP-limit probe and `loadClientLimits` now consider `client_tariffs.limit_ip_override` and tariff-sourced limits.

**Xray integration** — no direct changes in `internal/xray/`; the effect flows through `internal/web/service/xray.go` `GetXrayConfig` which uses `ListForInbound` — the generated xray client config receives the tariff-resolved `totalGB`/`expiryTime`/`limitIp` and inbound assignments.

**Subscription integration** — no direct changes in `internal/sub/`; `ListForInboundBySubId` (used by the raw/JSON/Clash sub server) now returns effective values, so subscription output reflects tariff-resolved quota/expiry/inbounds.

**MTProto integration** — no direct changes in `internal/mtproto/`; `InstanceFromInbound` reads each client's `totalGB`/`expiryTime` from the inbound's settings JSON, which is written from the effective client model, so mtg `[secret-limits]` quota/expiry inherit tariff-effective values indirectly.

**i18n** — `internal/web/translation/*.json` (all 13 locales) gained `menu.tariffs`, `pages.profiles.*`, `pages.tariffs.*`, and `pages.clients.*` keys (`managedFieldLocked`, `managedFieldLockedDesc`, `tariffManagedNotice`, `makeLocal`, `returnToTariff`). Large line churn is JSON reformatting (all 13 files rewritten with consistent indentation/sorting).

### Frontend

- `src/pages/groups/GroupsPage.tsx` — three-tab container (Groups / Tariffs / Profiles), shared query hooks and inbound options.
- `src/pages/groups/GroupsTab.tsx` — group table with tariff column, create/rename (with tariff picker), delete, reset traffic, **reset tariff**, add/remove clients, sub-links, bulk adjust.
- `src/pages/groups/GroupFormModal.tsx` — create/rename modal with name input + tariff picker.
- `src/pages/groups/TariffsTab.tsx` + `TariffFormModal.tsx` — tariff CRUD, ordered profile-chain composer (add/search/reorder/remove profiles), per-field strategy selects, and a live **resolved preview** showing effective traffic/expiry/IP/inbounds and their source profile.
- `src/pages/groups/ProfilesTab.tsx` + `ProfileFormModal.tsx` — profile CRUD (traffic/expiry/IP/inbounds), `tariffCount` usage column.
- Reuses pre-existing `GroupAddClientsModal.tsx` / `GroupRemoveClientsModal.tsx` for bulk membership (not created by this PR — wired into `GroupsTab`).
- `src/components/ManagedField.tsx` + `src/hooks/useTariffOverrides.ts` — render a locked/overlayed field when tariff-managed with a lock icon and a "make local" escape hatch via click popover.
- `src/pages/clients/ClientFormModal.tsx` — tariff-aware form: managed `totalGB`/`limitIp`/`expiryTime`/inbounds are locked behind `ManagedField`, show the tariff name tag, can be made local or returned to tariff; expiry/delayed-start interplay handled.
- `src/pages/clients/ClientsPage.tsx` — edit-hydration merge order flipped (`{...full.client, ...row}`) so override/slim fields survive; group props across modals changed from `string[]` to `{name: string}[]`.
- `src/lib/tariff/resolveChain.ts` — TS twin of the Go resolver for the TariffFormModal preview.
- `src/schemas/tariff.ts`, `src/schemas/profile.ts` — Zod schemas; `src/schemas/client.ts` extended `ClientRecordSchema` (override fields, `tariffName`, `*IsOverridden`) and `GroupSummarySchema` (tariff info).
- `src/api/queryKeys.ts` — `keys.clients.tariffs()` / `keys.clients.profiles()`; `src/layouts/AppSidebar.tsx` — `/groups` nav active-state handling.
- `src/pages/api-docs/endpoints.ts` — new "Tariffs" and "Profiles" sections + `get/effective/:email`, `groups/resetTariff`, `overrideField`, `returnToTariff` entries.
- `src/generated/*` — regenerated via `tools/openapigen/main.go` (new `StructAllow` entries: `Tariff`, `Profile`, `TariffProfile`, `TariffProfileItem`, `ResolvedFields`, `TariffSummary`).
- 
## How was this tested?

- Frontend: `npm run typecheck` and `npm run lint` pass; `npm run build` (Vite production bundles) succeeds. `npm run gen` was run — the `src/generated/` and `public/openapi.json` diffs are the regeneration output of the new Go structs.
- Frontend unit tests: `npm run test` — two existing golden snapshots (`src/test/__snapshots__/headers.test.ts.snap`, `inbound-defaults.test.ts.snap`) grew additively. The test source files did NOT change on this branch — the snapshot additions are from test cases that already existed in the v3.6.0 source but whose snapshots were stale (never regenerated after a prior change). The new entries are all additive `exports[...]` blocks at the end of each file.
- Backend: `go build ./...` and `go vet ./...` pass. `golangci-lint run ./...` passes with zero issues. New tests in `internal/web/service/client_tariff_test.go`:
  - `TestResolveChain` (11 table-driven cases): empty chain, single/multi profile, overwrite/sum traffic strategies, union/overwrite inbound strategies, null field skipping, multi-field mixed profiles.
  - `TestResolveOverrides` (5 cases): no tariff, tariff values, override beats tariff, override without tariff, tariff expiry with `started_at`.
  - `TestEffectiveFunctions` (6 DB-backed cases): `EffectiveLimitIP`, `EffectiveTotalGB`, `EffectiveExpiryTime` resolve through real SQLite with tariff chain, override/return, and no-tariff fallback paths.
  - `TestSqlEffectiveMatchesGoResolver` (5-client integration test): verifies `sqlEffTotalGB`/`sqlEffExpiry`/`sqlEffLimitIP` (raw SQL expressions from `client_paging.go`) produce identical results to `EffectiveTotalGB`/`EffectiveExpiryTime`/`EffectiveLimitIP` (Go resolver) on the same SQLite data, across overwrite tariff, sum tariff, override, no-tariff, and no-group client scenarios.
- Frontend tests: `src/test/useTariffOverrides.test.tsx` (10 cases), `src/test/managed-field.test.tsx` + `src/test/units.test.ts` + `src/test/strategies.test.ts` (13), `src/test/client-form-tariff.test.tsx` and `src/test/client-form-tariff-detail.test.tsx` (tariff-aware form), `src/test/form-modals-tariff.test.tsx` + `src/test/client-info-modal.test.tsx` (tariff integration), `src/test/client-form-resolve.test.tsx` (resolution). Updated snapshots in `headers.test.ts.snap` and `inbound-defaults.test.ts.snap` (additive only).
- Manual verification from the commit history: tariff changes restart xray on inbound-affecting edits; traffic totals refresh on membership changes and tariff edit; `client_tariffs.started_at` keeps expiry stable across create/update/rename/bulk-add paths; GB↔bytes unit handling (traffic is stored in GB and converted with `1<<30`); tariff unbind instantly restores every client's own field values and inbound assignments with no residual side effects.
- `internal/web/job/check_client_ip_job.go` — fail2ban IP-limit enforcement adapted to the new model: `hasLimitIp()` and `loadClientLimits()` now resolve tariff-derived IP limits through `tariff_profiles` → `profiles.limit_ip`, walking ordered profile chains per tariff ID. The old code only read `clients.limit_ip` and had no awareness of tariff-sourced limits — without this change, fail2ban would not enforce IP limits set through tariffs. Added helper `resolveTariffLimitIPs()` that bulk-loads chains and resolves per tariff with last-wins semantics.

## Screenshots / recordings

N/A.

## Breaking changes

None for existing users. Migration is purely additive — four new tables and new columns on existing tables via GORM AutoMigrate. Existing rows are untouched, no data migration required. The groups API shape is backwards-compatible: `POST /groups/create` gains an optional `tariffId` field (unchanged callers still work), and `GET /groups` returns extra `tariffId`/`tariffName`/`tariff` fields (additive).

## Layers

| Layer | Files | +lines | −lines |
|-------|-------|--------|--------|
| Core/Backend (Go) | 24 | 2 711 | 359 |
| Frontend (React) | 28 | 2 444 | 673 |
| Go tests | 16 | 5 521 | 26 |
| Frontend tests | 13 | — | — |
| i18n (13 locales) | 13 | 1 170 | 53 |
| Generated (openapi, zod, types, msw) | 7 | 5 883 | 2 224 |
| **Total** | **101** | **17 729** | **3 335** |

The Generated layer is produced by `make gen` and contains zero hand-written code.

## Checklist
- [x] I tested the change locally and confirmed the described behavior.
- [x] I added or updated tests for the new behavior (when applicable).
- [x] `go build ./...` and the test suite pass locally.
- [x] For frontend changes: `npm run lint`, `npm run typecheck`, and `npm run build` pass.
- [x] I updated the Wiki / README / API docs if user-facing behavior changed.
- [x] My commits follow the project's existing message style.
- [x] I have no unrelated changes mixed into this PR.
