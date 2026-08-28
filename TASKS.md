# Аудит ветки feat/group-tariff-profiles — задачи

## HIGH

- [x] **F1 (T5)** — `listForInboundFiltered`: per-client `ResolveClientFields` → batch-resolve через кеш групп. **СДЕЛАНО**

## MEDIUM

- [x] **F2 (T6)** — SQL vs Go дивергенция. **СДЕЛАНО** (HasTraffic/HasExpiryDays/HasLimitIP в EffectiveConfig)
- [x] **F3 (T7)** — `ResolveForGroup`: двойной резолв. **СДЕЛАНО** (TariffExpiryDays в ResolvedClientFields)
- [x] **F4 (T8)** — `applyInboundList` ломает union-стратегию + мёртвый код. **СДЕЛАНО**
- [x] **F5 (T9)** — Подписка N+1 `ResolveClientFields`. **СДЕЛАНО** (ResolveGroupTariffs batch, getInboundsBySubId + AggregateTrafficByEmails)
- [x] **F6 (T10)** — `resolveEffectiveTraffic` игнорирует tx. **СДЕЛАНО** (db *gorm.DB в ResolveClientFields)
- [x] **F7 (T11)** — `OverrideField`: `ResolveClientFields` на каждый case. **СДЕЛАНО**
- [x] **F8 (T12)** — `rewriteTrafficForClients`: per-client цикл. **СДЕЛАНО** (ResolveGroupTariffs)
- [x] **F9 (T13)** — Дубликат override-reset блока в `AddToGroup` / `Update`. **СДЕЛАНО** (resetClientOverrides)

## LOW

- [x] **F10** — union/overwrite merge в 3 копиях. **СДЕЛАНО** (mergeInboundIds)
- [x] **F11** — `buildClientPayload` raw/effective split. **СДЕЛАНО** (комментарий)
- [x] **F12** — `tariffIdsContainingInbound` per-tariff цикл. **СДЕЛАНО** (batch profiles)
- [x] **F13** — `ToClientEffective`: резолвит 4 поля, использует 3. **СДЕЛАНО** (ResolveClientLimits)

## REFACTOR

- [ ] **T14** — Заменить `tariff_started_at` на таблицу `client_tariffs` (client_id, tariff_id, started_at, ended_at)
