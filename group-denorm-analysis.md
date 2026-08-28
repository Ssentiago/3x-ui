# Group name denormalization analysis

## What `group` means

`group` is a logical label on a client — a string in `clients.group_name`.
There is **no FK**, no cascading relationship, no config-level connection
between groups and clients. It's the moral equivalent of a tag.

The label is stored in three places, all redundant:

| Location | Reader | Purpose |
|---|---|---|
| `client_groups.name` | Join on string, `tariff_id` lives here | Canonical row. Tariff binding. |
| `clients.group_name` | All client list/detail queries, filters | Which clients are in group X. |
| `inbounds.settings.clients[].group` | **Nobody** | Dead. xray ignores it. |

## xray proof

`internal/web/service/xray.go:207` builds every protocol's client entry as:
```go
entry := map[string]any{"email": c.Email}
```
Then adds `id`, `flow`, `password`, `security`, `auth`, `publicKey`, etc. per protocol.
`group` is **never added**. xray has no concept of groups.

`inbounds.settings.clients[].group` is only read by `client_groups.go` itself —
to find entries to rewrite. A self-contained cycle of dead code.

## Dead code: inbound settings JSON propagation

Two functions walk every inbound's settings JSON to write/rename `group`:

1. **`AddToGroup`** (lines 345-389): for each affected client, finds matching email in inbound settings, writes/deletes `cm["group"]`. ~45 lines.

2. **`replaceGroupValue`** (lines 429-491): for each affected client, finds entries where `cm["group"] == oldName`, renames to `newName`. ~60 lines.

Total: ~105 lines of dead code, executed inside DB transactions on every group rename
and every client-group membership change.

## What to do

### 1. Kill JSON propagation

Remove the inbound-settings-update loops from `AddToGroup` and `replaceGroupValue`.
Also remove the `inboundIDs` collection that only fed those loops.

### 2. Kill all `affected` counts

Since groups and clients have no structural relationship (no FK, no config dependency),
counting "affected clients" is meaningless. Renaming a group is just renaming a tag.
Deleting a group is just deleting a row. Adding a client to a group is just setting
a label.

Change return types:

| Function | Before | After |
|---|---|---|
| `RenameGroup` | `(int, error)` | `error` |
| `DeleteGroup` | `(int, error)` | `error` |
| `AddToGroup` | `(int, error)` | `error` |
| `RemoveFromGroup` | `(int, error)` | `error` |
| `replaceGroupValue` | `(int, error)` | `error` |

### 3. Backend: drop `affected` from responses

In `group.go`:
- `rename`: remove `affected` from response, return just `success`
- `delete`: remove `affected` from response, return just `success`
- `bulkAdd`/`bulkRemove`: remove `affected` from response

Also removes the `affected := 0` / `else` counting problem entirely —
no count is returned, no toast needs to display one.

### 4. Frontend: simple toasts without counts

In `GroupsTab.tsx`:
- `confirmRename`: show "Group renamed" (or `${name}` with tariff info), no count
- `onDelete`: show "Group deleted", no count
- Bulk add/remove: show simple success, no count

### 5. Update docs

`frontend/src/pages/api-docs/endpoints.ts` currently claims endpoints return
"the number of clients whose label was updated". Remove that claim.

## Files touched

| File | Change |
|---|---|
| `internal/web/service/client_groups.go` | Remove JSON loops (~120 lines). `(int, error)` → `error`. |
| `internal/web/controller/group.go` | Drop `affected` from response bodies. |
| `frontend/src/pages/groups/GroupsTab.tsx` | Simplify toasts, remove `affected` parsing. |
| `frontend/src/pages/api-docs/endpoints.ts` | Remove "number of clients" from endpoint descriptions. |

## Migration concern

None. Old inbounds with stale `"group"` entries in settings JSON are harmless —
xray ignores unknown keys. No DB schema change.

## Side fix

The tariff-only rename bug (toast shows "0 clients") disappears automatically —
there is no count to show anymore. The tariff IS applied through
`group.go:129-136`, which is the real work, and it runs regardless.

---

## Implementation guide (step by step)

### Step 1: `internal/web/service/client_groups.go`

#### 1a. `AddToGroup` — kill JSON loop

Delete lines 329-389 (the `inboundIDs` collection + the `for _, ibID := range inboundIDs` loop that writes `cm["group"]` into inbound settings). The `tx.Commit()` + `return len(records), nil` at the bottom becomes `return tx.Commit().Error` (no count).

Before deletion, the function body looks like:
```
Line 285: tx := db.Begin()
Line 286-293: UPDATE clients SET group_name = ? WHERE email IN ?
Line 295-328: tariff_started_at seeding + override clearing
Line 329-337: collect inboundIDs from client_inbounds JOIN    ← DELETE
Line 339-343: build emailSet                                     ← DELETE
Line 345-389: for each inbound, parse JSON, write cm["group"]   ← DELETE
Line 391-393: tx.Commit()
Line 394:     return len(records), nil                           ← CHANGE to return tx.Commit().Error
```

Watch out: `inboundIDs` is a `[]int` declared inside the `if group != ""` block on line 295 — it's only declared at line 296. Check where it's declared vs where it's used.

**Actually, re-read carefully** — there are TWO inbound-update loops in this file:
- One in `AddToGroup` (lines 329-389) — writes `cm["group"]` for added/removed clients
- One in `replaceGroupValue` (lines 429-491) — renames `cm["group"]` for renamed groups

#### 1b. `replaceGroupValue` — kill JSON loop + return type

Delete lines 429-491 (the `inboundIDs` collection + the for loop). 

Before:
```go
// Line 420: tx := db.Begin()
// Line 422-424: UPDATE clients SET group_name = ?
// Line 429-447: collect inboundIDs
// Line 449-491: for each inbound, parse JSON, rename cm["group"]
// Line 493: tx.Commit()
// Line 496: return len(records), nil
```

After:
```go
tx := db.Begin()
if err := tx.Model(&model.ClientRecord{}).
    Where("group_name = ?", oldName).
    UpdateColumn("group_name", newName).Error; err != nil {
    tx.Rollback()
    return err
}
return tx.Commit().Error
```

Change signature: `func (...) replaceGroupValue(oldName, newName string) (int, error)` → `func (...) replaceGroupValue(oldName, newName string) error`

#### 1c. Update callers of changed signatures

**`RenameGroup` (line 217):**
```go
// Before:
func (s *ClientService) RenameGroup(oldName, newName string) (int, error) {
    ...
    return s.replaceGroupValue(oldName, newName)      // returns (int, error)
}

// After:
func (s *ClientService) RenameGroup(oldName, newName string) error {
    ...
    return s.replaceGroupValue(oldName, newName)      // returns error
}
```
Also remove `if oldName == newName { return 0, nil }` → `if oldName == newName { return nil }`

**`DeleteGroup` (line 232):**
```go
// Before:
func (s *ClientService) DeleteGroup(name string) (int, error) {
    ...
    return s.replaceGroupValue(name, "")
}

// After:
func (s *ClientService) DeleteGroup(name string) error {
    ...
    return s.replaceGroupValue(name, "")
}
```

**`AddToGroup` (line 244):**
```go
// Before:
func (s *ClientService) AddToGroup(emails []string, group string) (int, error) {
    ...
    return len(records), nil   // at the end

// After:
func (s *ClientService) AddToGroup(emails []string, group string) error {
    ...
    // All early returns: return 0, err → return err
    // All early returns: return 0, nil → return nil
    return tx.Commit().Error   // at the end
}
```

**`RemoveFromGroup` (line 240):**
```go
// Just calls AddToGroup(emails, ""). Already returns (int, error).
// Change to: return s.AddToGroup(emails, "")  (now returns error)
func (s *ClientService) RemoveFromGroup(emails []string) error {
    return s.AddToGroup(emails, "")
}
```

#### 1d. Check all callers of these functions

Search for `RenameGroup`, `DeleteGroup`, `AddToGroup`, `RemoveFromGroup` across the codebase. Callers to update:

- `internal/web/controller/group.go` — calls all four
- `internal/web/controller/client.go` — might call AddToGroup/RemoveFromGroup
- `internal/web/service/api_scale_postgres_test.go:105` — calls RenameGroup
- Any test files that call these

### Step 2: `internal/web/controller/group.go`

#### 2a. `rename` handler (line 96)

```go
// Before (line 103-106):
affected := 0
if body.OldName != body.NewName {
    var err error
    affected, err = a.clientService.RenameGroup(body.OldName, body.NewName)
    if err != nil {
        jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
        return
    }
}

// After:
if body.OldName != body.NewName {
    if err := a.clientService.RenameGroup(body.OldName, body.NewName); err != nil {
        jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
        return
    }
}
```

Line 138: `jsonObj(c, gin.H{"affected": affected}, nil)` → `jsonObj(c, gin.H{}, nil)`

#### 2b. `delete` handler (line 146)

```go
// Before (line 152):
affected, err := a.clientService.DeleteGroup(body.Name)
...
jsonObj(c, gin.H{"affected": affected}, nil)

// After:
if err := a.clientService.DeleteGroup(body.Name); err != nil {
    jsonMsg(c, ...)
    return
}
jsonObj(c, gin.H{}, nil)
```

#### 2c. `bulkAdd` (line ~180) and `bulkRemove` (line ~220)

Same pattern — drop `affected` from response. Check exact line numbers.

### Step 3: `internal/web/controller/client.go`

Check if any client endpoints call `AddToGroup`/`RemoveFromGroup`. If so, update return value handling from `(int, error)` to `error`.

### Step 4: Frontend `GroupsTab.tsx`

#### 4a. `confirmRename` (line 180)

```tsx
// Before (line 198-201):
if (msg?.success) {
    const affected = (msg.obj as { affected?: number } | undefined)?.affected ?? 0;
    messageApi.success(t('pages.groups.editSuccess', { count: affected }));
    setRenameOpen(false);
}

// After:
if (msg?.success) {
    messageApi.success(t('pages.groups.editSuccessSimple'));  // or reuse editSuccess without {count}
    setRenameOpen(false);
}
```

Check if `pages.groups.editSuccess` has a `{count}` placeholder. If so, need a new key or change the template. Look at the en-US locale to see the current string. If it's "Group change applied to {count} clients", change to "Group updated" without count.

#### 4b. `onDelete` (line 205)

```tsx
// Before (line 213-216):
const msg = await deleteMut.mutateAsync({ name: g.name });
if (msg?.success) {
    const affected = (msg.obj as { affected?: number } | undefined)?.affected ?? 0;
    messageApi.success(t('pages.groups.deleteSuccess', { count: affected }));
}

// After:
const msg = await deleteMut.mutateAsync({ name: g.name });
if (msg?.success) {
    messageApi.success(t('pages.groups.deleteSuccessSimple'));  // or reuse without count
}
```

#### 4c. Bulk add/remove handlers

Same pattern — drop `affected` parsing from success toasts.

### Step 5: i18n keys

Check what the current toast keys look like. If they use `{count}`, either:
- Change the key value to a simple message without interpolation, OR
- Add a new key (e.g., `pages.groups.editSuccessSimple`) and keep the old one for delete

Need to update all 13 locale files if keys change. Check `internal/web/translation/en-US.json` first.

### Step 6: `frontend/src/pages/api-docs/endpoints.ts`

Find the rename/delete/bulkAdd/bulkRemove endpoint entries and remove claims about "returns the number of clients":

- Line ~741: `Add many clients to a group` — remove "Updates ... inbound's settings JSON"
- Line ~748: `Clear the group label` — remove "inbound's settings JSON"
- Line ~806: `Rename a group` — remove "propagated to ... inbound's settings JSON" and "Returns the number of clients"

### Step 7: Tests

Check for test breakages:
- `internal/web/service/client_groups.go` — no test file exists (all testing is through integration)
- `internal/web/service/api_scale_postgres_test.go:105` — calls `RenameGroup`, needs signature update
- Frontend: no tests directly test group rename toasts, but `npm test` should still pass

### Step 8: Verify

- `go build ./...`
- `go vet ./...`  
- `npm run typecheck`
- `npm run lint`
- `npm test`

### Watch out

1. **`AddToGroup` has the `group != ""` scope block** (line 295): the `inboundIDs` declaration might be inside this block. Make sure to only delete the JSON-loop parts, not the `tariff_started_at` seeding or override clearing that also happen inside that block.

2. **Import cleanup**: after removing the JSON loops, `"encoding/json"` might become unused in `client_groups.go`. Check imports.

3. **`tx.Rollback()` calls inside deleted loops**: removing the loops removes their `tx.Rollback()` calls. Make sure the remaining transaction still has proper rollback on errors.

4. **`RemoveFromGroup` is a one-liner**: `func ... RemoveFromGroup(emails []string) (int, error) { return s.AddToGroup(emails, "") }`. Just change the signature, the body stays.

5. **The `affected` in `delete` handler is also used for nothing else** — just the JSON response. Safe to drop.
