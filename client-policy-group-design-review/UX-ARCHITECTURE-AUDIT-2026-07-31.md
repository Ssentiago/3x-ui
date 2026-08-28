# Audit: Client Group Tariffs

**Branch:** `feat/client-group-tariffs`  
**Scope:** UX-flow, ownership of client settings, and integration alternatives.  
**Date:** 2026-07-31

## Executive conclusion

The branch solves the central business task: an administrator can define a tariff once and use it for client groups. However, the implementation currently combines two incompatible models:

1. tariffs are live, read-time sources of effective client values;
2. client editing and several runtime jobs still treat raw client fields as the source of truth.

As a result, the interface can show an operator one value while enforcement, filtering, or a subsequent save uses another. This is more serious than a cosmetic UX issue: the promise of centralized management is not reliable yet.

For the stated scenario — three cohorts of one hundred independently created clients, each cohort needing centrally changed settings — the best long-term direction is **a live tariff assigned to a group, with per-client exceptions and an explicit impact preview for membership-changing actions**. It keeps `Client` the primary entity and makes a tariff a secondary source of defaults/effective values rather than a competing owner.

Before expanding the feature, the implementation needs one authoritative resolution path used by UI, API, reporting, and runtime enforcement.

## Actual model in this branch

```text
Tariff
  └─ ClientGroup.tariff_id
       └─ ClientRecord.group_name
            ├─ total_gb_override
            ├─ limit_ip_override
            ├─ expiry_time_override
            └─ inbounds_override
```

There is no `clients.tariff_id`. A client receives a tariff only through an explicit stored group. The effective value formula in the read path is:

```text
effective(field) = client override ?? group tariff value ?? unrestricted/default
```

This is a deliberate and reasonable `Group → Tariff → Client` model, but it differs from the older design proposal that also stored a tariff reference on each client. That difference must be intentional and documented: it means a group is not merely an organizational label; it is the only binding mechanism for a tariff.

## User flow: managing three cohorts

### Happy path

1. The administrator opens **Groups**.
2. They create `Retail`, `Partners`, and `Trial` groups.
3. They create tariffs such as `Retail 100 GB`, `Partner 500 GB`, and `Trial 7 days`.
4. They assign one tariff to each group.
5. They add the 100 corresponding clients to each group.
6. A client page shows the effective quota, expiry, IP limit, and allowed inbounds inherited from the group tariff.
7. Later, the administrator changes `Partner 500 GB` once.
8. All members of groups using that tariff should receive the new effective value; individual exceptions remain exceptions.

This flow is conceptually good for the goal: manage a cohort once, keep individual clients as the records that carry traffic, credentials, state, and optional exceptions.

### What the operator expects at each step

| Operator intent | Expected result | Current risk |
| --- | --- | --- |
| Change a tariff | Know exactly which groups and clients will be affected | Tariff list shows only group count, not affected client count |
| Move a client into a tariff group | New cohort settings become visible and enforceable | Existing per-client overrides are silently cleared on bulk add |
| Make one exception | The entered value becomes the client’s effective setting | The override value is snapshotted, while ordinary save writes a different raw field |
| Return an exception to tariff | Only this client returns to inherited values | Returning inbound access re-applies inbounds to the whole group |
| Remove a tariff from a group | Clients retain a clearly stated outcome | Two removal paths differ in runtime restart behavior |
| Edit a group name | Rename only | The same dialog also changes tariff assignment and can affect all members |

## Attempted UX breakage

### 1. “I need an exception for one client” — broken

The current Override action copies the current effective value into an override column. The form then lets the operator type a different value, but normal client save writes to the raw `limit_ip`, `total_gb`, and `expiry_time` columns. Effective-value resolution ignores those raw columns while the override is non-null.

Example: `Gold` has an IP limit of 3. The operator presses Override, enters 5, and saves. The form can persist raw `limit_ip = 5`, but `limit_ip_override` remains 3, so effective resolution still yields 3.

**User impact:** the main exception workflow appears to succeed but retains the old effective value.

**Evidence:** `internal/web/service/client_tariff.go:155`, `internal/web/service/client_crud.go:459`.

### 2. “I reopened the client to check the exception” — broken

The client edit form checks `limitIp` when deciding whether it has an override, but the predicate expects `limitIP`. The mismatch makes the check always false. The tariff value can overwrite the form field even when the client has a personal IP limit.

Inbound overrides have a similar problem: the form re-seeds tariff inbounds without checking whether `inbounds_override` is set.

**User impact:** an operator cannot confidently inspect or edit a personal exception; simply opening and saving the client may destroy what they intended.

**Evidence:** `frontend/src/pages/clients/ClientFormModal.tsx:246`, `frontend/src/pages/clients/ClientFormModal.tsx:269`, `frontend/src/pages/clients/ClientFormModal.tsx:271`.

### 3. “The dashboard says this client is active, so the limit is fine” — broken

The client list displays effective values, but depletion, expiration filters, summary cards, and sorting use raw `clients.total_gb` and `clients.expiry_time` in SQL. A tariff-managed client can display a tariff deadline while not appearing in the expiring/depleted filter.

**User impact:** the central-management page gives a misleading operational picture exactly when the administrator needs to identify cohorts requiring attention.

**Evidence:** `internal/web/service/client_paging.go:184`, `internal/web/service/client_paging.go:235`, `internal/web/service/client_paging.go:323`.

### 4. “The tariff says three IPs; therefore three IPs are enforced” — broken

The fail2ban IP-limit job loads `clients.limit_ip`, not the resolved tariff value. For a tariff-managed client whose raw value is zero, the UI can show an IP limit while the enforcement job treats the client as unlimited.

**User impact:** a tariff can be visually persuasive but operationally ineffective.

**Evidence:** `internal/web/job/check_client_ip_job.go:122`, `internal/web/job/check_client_ip_job.go:145`.

### 5. “I changed a tariff; therefore all clients now have the new limit” — ambiguous/broken

The read paths resolve GB, expiry, and IP values from the tariff, but other runtime paths and traffic records retain raw values. Changing a tariff’s inbound list also immediately walks every client in every attached group and changes their inbound bindings; errors are collected but not returned to the operator.

**User impact:** a seemingly simple tariff edit has hidden mass side effects for inbounds, while quota and expiry propagation is not consistently reflected by enforcement and operational jobs.

**Evidence:** `internal/web/service/tariff.go:107`, `internal/web/service/tariff.go:141`, `internal/web/service/client_tariff.go:72`.

### 6. “Return this one client to the tariff” — broken scope

Returning the `inbounds` field to a tariff clears the selected client’s override flag, then applies the tariff inbound list to every non-overridden client in the group.

**User impact:** an action framed as a one-client correction can alter other clients’ subscriptions and access.

**Evidence:** `internal/web/service/client_tariff.go:199`.

### 7. “I only want to rename a group” — risky flow

The edit-group dialog combines a group rename and tariff assignment/removal. A tariff assignment may modify client inbound bindings and schedule an Xray restart, but the confirmation does not show the number of clients or affected fields. The separate “Remove tariff” action follows a different backend route and does not schedule the restart.

**User impact:** two UI paths for the same conceptual operation behave differently, and the risky action is hidden inside a low-risk edit task.

**Evidence:** `frontend/src/pages/groups/GroupsPage.tsx:250`, `internal/web/controller/group.go:94`, `internal/web/controller/group.go:162`.

### 8. “Delete this obsolete tariff” — dead end

The UI disables Delete whenever a tariff is in use. It provides no direct way to see the linked groups or to detach/reassign them from the confirmation.

**User impact:** the operator must backtrack and manually find all groups; this is especially frustrating with many tariffs.

**Evidence:** `internal/web/service/tariff.go:157`, `frontend/src/pages/groups/GroupsPage.tsx:739`.

## UX improvements that preserve the current entity hierarchy

These changes do not make Tariff the primary entity and do not require a tariff reference on the client.

1. Keep **Groups** as the operational home. A group row should show its tariff inline, member count, and a concise summary of managed fields.
2. Split **Rename group** from **Assign/change/remove tariff**. Changing tariff should have its own action and confirmation.
3. Before assigning/changing/removing a tariff, show an impact preview: affected groups, affected clients, fields to be managed, inbound changes, and how existing exceptions are handled.
4. Show both `groupCount` and aggregate `clientCount` on a tariff. Make the counts navigable to the relevant group/client filter.
5. In the client table, show a tariff badge next to the group and distinguish inherited values from personal values.
6. In the client form, show a field-level source: `Inherited from Gold`, `Individual value`, or `No tariff`. The action should read `Make individual` / `Use Gold again`, not merely `Override`.
7. For inbounds, never use a group-wide side effect as the implementation of a per-client button. Display inherited inbound tags read-only until the user explicitly makes a personal exception.
8. Treat a tariff edit as a potentially broad change. Save the template first; use a separate, explicit **Sync inbound access** operation if inbounds are part of the tariff. Its confirmation must state scope and report partial failures.
9. Add an explanation for zero values: `0 GB = unlimited`, `0 days = never expires`, `0 IPs = unlimited`. An empty inbound list should explicitly mean either “do not manage inbounds” or “deny all”; the product must pick one meaning and use it consistently.

## Architectural options without changing Client as the primary entity

### Option 1: Live group tariff with read-time resolution — recommended after repair

```text
Tariff → Group → Client
Client overrides specific tariff-managed fields
```

Tariff remains a reusable definition. Group selects the tariff. Client remains the record of identity, credentials, traffic, status, and optional exceptions. All consumers resolve values through one shared resolver or through a materialized projection updated atomically.

**Best for:** the stated three cohorts and ongoing central changes.

**Benefits:** one tariff edit affects all relevant cohorts; reuse across groups; exceptions do not fork the group model.

**Costs:** every consumer must use resolved values. Inbound membership is a side effect that needs an explicit synchronization contract.

**Required design choice:** decide whether raw client fields are fallback/manual values or whether `_override` columns are the only exception store. Do not keep both as competing sources.

### Option 2: Reusable tariff as an explicit batch template

```text
Tariff → selected groups/clients → explicit Apply → copy values into Client
```

The tariff is a saved preset, not a live parent. Pressing Apply creates a bulk operation with preview, field selection, and an audit result. Clients fully own their resulting values after the copy.

**Best for:** operators who value safety, snapshots, and predictable changes more than always-live synchronization.

**Benefits:** simple ownership; no resolver in fail2ban, filtering, or runtime; no per-field inheritance state.

**Costs:** a tariff update does nothing until an operator explicitly reapplies it. The user must remember to do so.

### Option 3: Group configuration, no reusable tariff library

```text
Group settings → members
```

Put the limits directly on a group. The group is the cohort configuration object; tariff is just a label or is removed.

**Best for:** exactly one distinct settings profile per group, with little re-use across groups.

**Benefits:** two entities and a natural mental model: “these are the settings of this cohort.”

**Costs:** duplicate configuration when two groups need the same setup; group becomes more than an organizational label.

### Option 4: Saved bulk-edit recipes

```text
Recipe (selected fields + values) → saved target query/group → explicit batch job
```

This is a lightweight automation feature rather than inheritance. A recipe can target a group, a filtered client set, or a named saved segment.

**Best for:** operations that are periodic but not truly continuous, such as quarterly quota changes or a temporary campaign.

**Benefits:** minimal coupling to Client; explicit history and rollback opportunities; can support heterogeneous target selections.

**Costs:** it is not a live tariff system and does not communicate durable membership-based entitlement.

### Option 5: Membership rules / computed segments

```text
Rule-defined segment → tariff or bulk recipe
Client remains primary; group membership is computed from client attributes
```

Instead of manually assigning each client to a named group, define a segment such as `comment contains reseller` or `inbound in [1, 2]`, then attach a tariff or run a batch job.

**Best for:** cohorts that can be described consistently from existing client data.

**Benefits:** removes repetitive manual membership maintenance; keeps Client data authoritative.

**Costs:** more complex query UX and surprising changes when a client attribute changes; inappropriate when membership is intentionally hand-curated.

## Recommendation for this branch

Adopt **Option 1** deliberately, rather than drifting toward it:

1. Define one authoritative effective-value source for every field.
2. Route UI display, client save, pagination/filtering, traffic enforcement, and IP-limit enforcement through that source.
3. Change the override API to store the value the operator entered, or teach normal client save to write to the corresponding override column for tariff-managed clients.
4. Separate individual inbound return from group inbound synchronization.
5. Split group rename from tariff assignment; add impact preview and confirm for every membership or tariff change that may change access.
6. Make inbound synchronization an explicit, observable operation with per-client errors and a restart contract.
7. Add end-to-end tests for the 100-client cohort scenario: assignment, tariff edit, individual override, return to tariff, move between groups, remove tariff, filtering, and enforcement.

If the team does not want to guarantee that every runtime consumer resolves effective values, choose **Option 2** instead. It is less dynamic but substantially safer than a partially live inheritance model.

## Evidence limits

This audit is based on the branch’s source code and user-flow reconstruction. It does not include a running-panel visual capture, production traffic data, or a performance benchmark of large inbound synchronization jobs. Those should be checked before a UX sign-off, especially for atomicity and runtime restart behavior.

## Repeat review after fixes

**Reviewed commit:** `fd631302` (`fix(tariff): wire effective values to enforcement, filtering, and client save`)  
**Date:** 2026-07-31

The follow-up work fixed several important paths:

- normal client save now writes an existing GB, IP, or expiry exception back into the corresponding override column;
- inbound form seeding no longer overwrites an explicit inbound exception;
- returning a client’s inbound field to the tariff now targets that client rather than every client in its group;
- client-list filtering, summary buckets, expiry filtering, and sorting now attempt to use tariff-aware SQL;
- tariff-driven IP limits are considered by the IP-limit job;
- changing tariff inbound access schedules an Xray restart;
- the tariff selector is searchable and the empty-inbound wording now explains that blank means “do not manage inbound assignments.”

These fixes are meaningful, but this is **not ready for sign-off**. The following issues are blockers for the central-management promise.

### P0: tariff quota is interpreted as bytes although the form labels it GB

Client traffic limits are stored and consumed in bytes. Tariff `totalGB`, however, is entered and displayed as GB and is copied as the plain numeric value into effective calculations and `client_traffics.total`.

```text
Tariff value: 100 GB
Stored/enforced after resolution: 100 bytes
Expected: 107,374,182,400 bytes
```

An operator can create a `100 GB` tariff, assign it to a group, and find that the client is exhausted after more than 100 bytes. This breaks quota enforcement, the client list, and subscription usage headers for every tariff-managed client.

**Evidence:** `internal/web/service/client_tariff.go:16`, `internal/web/service/client_paging.go:117`, `internal/web/service/inbound_traffic.go:524`, `internal/web/service/client_inbound_apply.go:1271`.

### P0: non-tariff clients resolve to unlimited and can lose their quota on edit

The Go effective-value helpers return zero when there is neither an override nor a tariff. They should fall back to the client’s raw fields. SQL filtering already has that fallback, so row display and filtering disagree.

Worse, the client page merges a hydrated full client record with the paged row, allowing the zero effective values to replace real raw limits. Opening a regular client with a quota, then saving without intending a change, can write a zero quota.

**Minimal scenario:** an ungrouped client with `10 GB` has `total_gb = 10 GiB`; the list renders unlimited; open and save the client; the raw limit becomes zero.

**Evidence:** `internal/web/service/client_tariff.go:16`, `internal/web/service/client_paging.go:479`, `frontend/src/pages/clients/ClientsPage.tsx:508`, `frontend/src/pages/clients/ClientFormModal.tsx:335`.

### P1: personal IP exception is still overwritten in the form and ignored by enforcement

The field predicate accepts `limitIP`, while form initialization asks for `limitIp`. Therefore it never recognizes an existing override and re-seeds the tariff IP limit. Separately, the IP enforcement job detects that an override exists but returns raw `limit_ip` instead of `limit_ip_override`.

**Minimal scenario:** tariff IP limit = 3; client exception = 5; reopening and saving the form can restore 3; the enforcement job reads the stale raw number rather than 5.

**Evidence:** `frontend/src/pages/clients/ClientFormModal.tsx:246`, `frontend/src/pages/clients/ClientFormModal.tsx:269`, `internal/web/job/check_client_ip_job.go:145`.

### P1: assigning an expiry tariff to existing clients can instantly disable them

Expiry is calculated as `client.created_at + tariff.expiry_days`. Moving a client created six months ago into a 30-day tariff group makes the computed expiry five months old. The client can be immediately marked exhausted and disabled.

The intended lifecycle must be chosen explicitly: normally tariff expiry starts when the client enters the group, when the tariff is assigned to the group, or at a separately stored entitlement start time. It should not silently use the account creation time unless that is the product rule.

**Evidence:** `internal/web/service/client_tariff.go:36`, `internal/web/service/client_paging.go:118`, `internal/web/service/inbound_traffic.go:539`.

### P1: changing/removing a tariff leaves operational traffic rows stale

`client_traffics.total` and `client_traffics.expiry_time` are refreshed only when a client is attached or updated. A tariff quota edit or tariff removal changes read-time resolution but does not refresh traffic rows. Disable jobs and subscription headers can therefore continue to enforce old values until an unrelated client update.

**Evidence:** `internal/web/service/inbound_traffic.go:493`, `internal/web/service/tariff.go:107`, `internal/web/controller/group.go:162`.

### P1: returning client inbound access to tariff may not take effect until another restart

The corrected per-client inbound operation attaches/detaches the client, but its restart requirement is discarded; its controller path never schedules Xray restart. The change is persisted but may not become active in the managed process right away.

**Evidence:** `internal/web/service/client_tariff.go:213`, `internal/web/controller/group.go:363`.

### Remaining UX and quality gaps

- Tooltips reference `pages.clients.overrideDesc` and `pages.clients.returnToTariffDesc`, but these keys are absent from all locales, so raw keys can be shown to users.
- There are no focused tests for tariff effective resolution, units, override edits, transfer into an expiry tariff, inbound return, or runtime traffic refresh.
- `Tariff.Enable` is stored and exposed but has no behavioral effect and no active UI control.
- A newly created client in a tariff group does not receive tariff inbound assignment through the create flow.

### Required next repair sequence

1. Establish units at the domain boundary: either store tariff traffic in bytes or convert GB to bytes exactly once before effective resolution and runtime writes.
2. Make Go effective functions follow `override → tariff → raw client value` consistently for every field.
3. Prevent paged effective display data from overwriting hydrated raw client data during edit.
4. Correct IP override field naming and return the override value in the enforcement job.
5. Introduce an explicit entitlement-start timestamp or a documented, user-visible expiry-start rule for group membership.
6. Atomically refresh `client_traffics` and schedule runtime reload wherever tariff membership or tariff limits change.
7. Add end-to-end tests before another review pass; the 100-client cohort scenario must cover UI-effective display, filtering, enforcement, individual exception, and tariff change/removal.

## Third review after `edb4dc04`

**Reviewed commit:** `edb4dc04` (`fix(tariff): GB-bytes units, fallback to raw, stale traffic, restart, naming`)  
**Date:** 2026-07-31

### Verified as fixed

- Tariff GB values are converted to bytes in the Go resolver, SQL list expressions, and traffic-row refresh path.
- Clients without a tariff now fall back to their raw GB, IP, and expiry values in the Go resolver.
- The `limitIP` spelling mismatch in form initialization is corrected.
- IP enforcement now uses `limit_ip_override` when a personal IP exception exists.
- Returning one client’s inbounds to the tariff requests an Xray restart.
- Updating a tariff refreshes quota and expiry in `client_traffics` for members without relevant exceptions.

### P0: expiry now has three incompatible meanings

The fix prevents an old account from expiring immediately after entering a tariff group by replacing `created_at + days` with `now + days` in the Go resolver. But the new value is calculated on every read instead of being persisted as an entitlement start.

```text
Client list/API resolver:       now + tariff days
Client-list SQL filtering:      client.created_at + tariff days
Traffic/runtime refresh:        now + tariff days at the moment of tariff update
```

The same client can therefore be shown with a moving future expiry in one place, classified as expired in another, and disabled according to a third timestamp. More importantly, a tariff expiry never actually expires in the read path: each page refresh moves its deadline forward by the tariff duration.

**Minimal scenario:** move a six-month-old client into a 30-day tariff. The table can display a date 30 days from the current moment; expiry filtering can classify it expired from the six-month-old creation date; after a tariff edit, runtime uses yet another “30 days from edit” date.

This must be resolved with a persisted timestamp, for example `tariff_started_at` on the client or a membership table. The effective formula then becomes `override ?? (tariff_started_at + tariff days) ?? raw expiry`, and SQL, runtime refresh, subscriptions, and the UI all use the same value.

**Evidence:** `internal/web/service/client_tariff.go:39`, `internal/web/service/client_paging.go:118`, `internal/web/service/tariff.go:161`, `internal/web/service/inbound_traffic.go:539`.

### P1: membership changes still do not refresh operational quota/expiry rows

`refreshTariffTraffic` runs only after editing a tariff. Moving clients into a tariff group, assigning a tariff to an existing group, removing a tariff from a group, removing a client from a group, or deleting a group changes the effective model without refreshing `client_traffics.total` and `client_traffics.expiry_time`.

This leaves runtime disabling and subscription accounting with prior limits until another tariff edit or client save. The intended central-management action must be atomic: update membership/binding, write the resulting traffic limits, modify inbound bindings where applicable, then schedule the runtime reload.

**Evidence:** `internal/web/service/tariff.go:138`, `internal/web/controller/group.go:94`, `internal/web/controller/group.go:162`, `internal/web/controller/group.go:200`, `internal/web/service/client_groups.go:246`.

### P2: create flow still bypasses tariff-controlled inbound access

Creating a client directly in a group with a tariff does not invoke the tariff inbound application path. The form also applies tariff defaults only in edit mode. The client can therefore be labelled as belonging to a tariff cohort while retaining arbitrary inbound access from the create payload.

**Evidence:** `internal/web/service/client_crud.go:45`, `frontend/src/pages/clients/ClientFormModal.tsx:266`.

### P2: verification coverage remains absent

The fixes change quota units, runtime enforcement, paging SQL, and expiry semantics, but no focused tariff tests cover those contracts. The expiry regression demonstrates why this cannot rely on a compile/type check alone.

### Current decision

The branch has closed the prior quota-unit and no-tariff fallback P0s. It still cannot receive a UX or operational sign-off because tariff expiry has no stable lifecycle boundary, and membership changes are not propagated atomically to runtime traffic state. Fix those two architectural contracts before another UI refinement pass.
