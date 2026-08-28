# Plan: Move overrides onto `client_tariffs` active row

## Current state (the problem)

Override data lives in two places:

```
clients.is_total_gb_overridden    BOOL   ← флаг «управляется не тарифом»
clients.total_gb                  INT    ← замороженное при freeze значение (мутирует)
```

`OverrideField("totalGB")` пишет effective value в `clients.total_gb` и ставит флаг. `clients.total_gb` теряет исходное «собственное» значение админа навсегда.
При смене тарифа `AddToGroup` вынужден вручную чистить четыре флага — иначе оверрайд от старого тарифа протухнет и зависнет.

## Proposed change — Option E

Перенести оверрайды на активную строку `client_tariffs`:

```sql
client_tariffs (
  id                   INTEGER PK,
  client_id            INTEGER NOT NULL,
  tariff_id            INTEGER NOT NULL,
  started_at           INTEGER NOT NULL,
  ended_at             INTEGER?,
  -- ↓ новые колонки ↓
  total_gb_override    INTEGER?,
  limit_ip_override    INTEGER?,
  expiry_time_override INTEGER?,
  is_inbounds_overridden BOOL DEFAULT false
)
```

Флаги `is_*_overridden` на `clients` удаляются. `clients.total_gb` / `limit_ip` / `expiry_time` больше не мутирует тарифная система.

## What this solves automatically

Оверрайд **физически не может пережить смену тарифа**. Когда старая строка получает `ended_at`, оверрайды закрываются вместе с ней — чистить руками ничего не нужно. Устраняется класс багов «клиент сменил тариф, старый оверрайд завис».

Если клиент возвращается на тот же тариф — старые оверрайды физически недостижимы (это плюс: membership-период новый, старые замороженные значения не имеют смысла).

## Schema diff

| Таблица | Удалить | Добавить |
|---------|---------|----------|
| `clients` | `is_total_gb_overridden`, `is_limit_ip_overridden`, `is_expiry_time_overridden`, `is_inbounds_overridden` | — |
| `client_tariffs` | — | `total_gb_override INTEGER?`, `limit_ip_override INTEGER?`, `expiry_time_override INTEGER?`, `is_inbounds_overridden BOOL DEFAULT false` |

`clients.total_gb` / `limit_ip` / `expiry_time` — больше не трогаются тарифной системой, навсегда остаются «собственным значением админа».

## Resolver

```
active = SELECT * FROM client_tariffs
         WHERE client_id = ? AND ended_at IS NULL

if active is None:
    return raw client values  // нет тарифа → нет оверрайдов

chain = resolveChain(active.tariff_id)

totalGB  = active.total_gb_override  ?? chain.traffic    ?? client.total_gb
limitIP  = active.limit_ip_override  ?? chain.limitIP    ?? client.limit_ip
expiry   = active.expiry_time_override ?? (active.started_at + chain.expiryDays*86400*1000)
inbounds = resolveInbounds(active.is_inbounds_overridden, chain, client)
```

Один JOIN вместо двух источников (было: `clients` для флагов + `client_tariffs` для `started_at`). Теперь всё лежит в одной строке.

## API changes

**`OverrideField(email, "totalGB")`:**
```sql
UPDATE client_tariffs
SET total_gb_override = <effective_totalGB>
WHERE client_id = ? AND ended_at IS NULL
```
Если активной строки нет (клиент без тарифа) → **400 Bad Request**. `OverrideField` для тарифных полей не имеет смысла без тарифа.

**`ReturnToTariff(email, "totalGB")`:**
```sql
UPDATE client_tariffs
SET total_gb_override = NULL
WHERE client_id = ? AND ended_at IS NULL
```
Аналогично — 400 если нет активной строки.

**`ReturnToTariff(email, "inbounds")`:**
```sql
UPDATE client_tariffs
SET is_inbounds_overridden = false
WHERE client_id = ? AND ended_at IS NULL
```
Плюс `applyInboundList` — без изменений.

## Что уходит из кода

- `clients.is_total_gb_overridden` / `is_limit_ip_overridden` / `is_expiry_time_overridden` / `is_inbounds_overridden` — 4 колонки + поля в `ClientRecord`
- `ClientSlim.IsTotalGBOverridden` etc. — флаги в slim-модели для пейджинга
- Ручная очистка флагов в `AddToGroup` / `resetClientOverrides` / `resetClientOverridesByEmails`
- `resetClientOverrides()` — функция, которая чистила флаги при смене тарифа (больше не нужна — `ended_at` делает это структурно)
- SQL-выражения `sqlEffTotalGB` / `sqlEffExpiry` / `sqlEffLimitIP` — упрощаются (JOIN на `client_tariffs` уже есть для `started_at`, добавляется COALESCE на override-колонки)

## Edge cases

1. **Клиент без тарифа вызывает OverrideField** → 400. Тарифные поля нечем переопределять.
2. **Клиент возвращается на тот же тариф** → старая строка с оверрайдами закрыта (ended_at), новая строка — чистая. Оверрайды не переносятся. Это by design.
3. **Миграция** → для всех активных строк `client_tariffs` (ended_at IS NULL): если на клиенте стоит `is_*_overridden = true`, перенести значение из `clients.total_gb`/`limit_ip`/`expiry_time` в override-колонку активной строки. После миграции удалить флаги с `clients`.
