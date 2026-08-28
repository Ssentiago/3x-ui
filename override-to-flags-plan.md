# Plan: nullable override columns → bool flags

## What

Replace 3 nullable `*_override` value columns + 1 nullable `inbounds_override *int` flag
with 4 `bool` flags. Values stay in existing client fields (`total_gb`, `limit_ip`,
`expiry_time`), no duplication.

```
             СЕЙЧАС                                     СТАНЕТ
total_gb_override    BIGINT NULL     is_total_gb_overridden    BOOL DEFAULT FALSE
limit_ip_override    INT NULL        is_limit_ip_overridden    BOOL DEFAULT FALSE
expiry_time_override BIGINT NULL     is_expiry_time_overridden BOOL DEFAULT FALSE
inbounds_override    INT NULL        is_inbounds_overridden    BOOL DEFAULT FALSE
tariff_started_at    BIGINT NULL     tariff_started_at         BIGINT NULL  ← не трогаем
```

## Почему

Сейчас значения дублируются: `total_gb` хранит fallback, `total_gb_override` — замороженное
тарифное. Флаги — достаточный механизм: `flag ? client.field : tariff_chain`.

## Файлы и изменения

### 1. `internal/database/model/model.go`

```go
// ClientRecord — удалить:
TotalGBOverride    *int64
LimitIPOverride    *int
ExpiryTimeOverride *int64
InboundsOverride   *int   // заменить на bool

// Добавить:
IsTotalGBOverridden    bool `gorm:"column:is_total_gb_overridden;default:false"`
IsLimitIPOverridden    bool `gorm:"column:is_limit_ip_overridden;default:false"`
IsExpiryTimeOverridden bool `gorm:"column:is_expiry_time_overridden;default:false"`
IsInboundsOverridden   bool `gorm:"column:is_inbounds_overridden;default:false"`

// ToClientEffective — параметры те же, логика та же (уже bool'и из override → флаг)
```

### 2. `internal/database/db.go` + `migrate_data.go`

GORM AutoMigrate сам добавит новые bool колонки и удалит старые.
Проверить: nullable → bool может вызвать ALTER TYPE. Возможно, нужна ручная миграция:
```sql
ALTER TABLE clients ADD COLUMN is_total_gb_overridden BOOLEAN DEFAULT FALSE;
ALTER TABLE clients ADD COLUMN is_limit_ip_overridden BOOLEAN DEFAULT FALSE;
ALTER TABLE clients ADD COLUMN is_expiry_time_overridden BOOLEAN DEFAULT FALSE;
ALTER TABLE clients ADD COLUMN is_inbounds_overridden BOOLEAN DEFAULT FALSE;
-- заполнить из старых override колонок:
UPDATE clients SET is_total_gb_overridden = TRUE WHERE total_gb_override IS NOT NULL;
UPDATE clients SET is_limit_ip_overridden = TRUE WHERE limit_ip_override IS NOT NULL;
UPDATE clients SET is_expiry_time_overridden = TRUE WHERE expiry_time_override IS NOT NULL;
UPDATE clients SET is_inbounds_overridden = TRUE WHERE inbounds_override IS NOT NULL;
-- потом удалить старые колонки
```

### 3. `internal/web/service/client_tariff.go` — ядро

**resolveOverrides** (строка 142):
```go
// Было:
if client.TotalGBOverride != nil { effectiveTotalGB = *client.TotalGBOverride }

// Стало:
if client.IsTotalGBOverridden { effectiveTotalGB = client.TotalGB }
// Аналогично limitIP, expiryTime
```

**resolveChain internal** (строка 186): то же самое — `!= nil` → bool check

**OverrideField** (строка 306):
```go
// Было: db.Update("total_gb_override", effective)

// Стало:
db.Updates(map[string]any{
    "total_gb": effective,
    "is_total_gb_overridden": true,
})
```

**ReturnToTariff** (строка 339):
```go
// Было: db.Update("total_gb_override", nil)

// Стало:
db.Update("is_total_gb_overridden", false)
```

**ApplyInboundListToGroup** (строка 231):
```go
// Было: WHERE inbounds_override IS NULL

// Стало: WHERE is_inbounds_overridden = FALSE
```

### 4. `internal/web/service/client_paging.go`

**ClientSlim** (строка 31-36):
```go
// Удалить: TotalGBOverride *int64, LimitIPOverride *int, ExpiryTimeOverride *int64
// Оставить: TotalGBIsOverridden bool, LimitIPIsOverridden bool, ExpiryIsOverridden bool
// Добавить: IsInboundsOverridden bool  (для полноты)
```

**sqlEffTotalGB** (строка 117):
```go
// Было: COALESCE(c.total_gb_override, ...)

// Стало: CASE WHEN c.is_total_gb_overridden THEN c.total_gb ELSE ...
```
Аналогично `sqlEffExpiry`, `sqlEffLimitIP`.

**pageRows fill** (строка 515-520):
```go
// Было:
TotalGBIsOverridden: rec.TotalGBOverride != nil,

// Стало:
TotalGBIsOverridden: rec.IsTotalGBOverridden,
```

**effectiveTotalGB, EffectiveExpiryTime, EffectiveLimitIP** (строки 708-729):
```go
// Было: if rec.TotalGBOverride != nil { return *rec.TotalGBOverride }

// Стало: if rec.IsTotalGBOverridden { return rec.TotalGB }
```

**resolveEffectiveInboundsForPage** (строка 743-767):
```go
// Было: rec.InboundsOverride != nil

// Стало: rec.IsInboundsOverridden
```

### 5. `internal/web/service/client_groups.go`

**AddToGroup** (строка 301-304):
```go
// Было:
Updates(map[string]any{
    "total_gb_override":    nil,
    "limit_ip_override":    nil,
    "expiry_time_override": nil,
    "inbounds_override":    nil,
})

// Стало:
Updates(map[string]any{
    "is_total_gb_overridden":    false,
    "is_limit_ip_overridden":    false,
    "is_expiry_time_overridden": false,
    "is_inbounds_overridden":    false,
})
```

### 6. `internal/web/service/tariff.go`

**refreshTariffTraffic** (строка 170-173):
```go
// Было: if clients[i].TotalGBOverride == nil

// Стало: if !clients[i].IsTotalGBOverridden
```
Аналогично ExpiryTimeOverride.

### 7. `internal/web/service/inbound_traffic.go`

**resolveEffectiveTraffic** (строка 529-535):
```go
// Было:
if client.TotalGBOverride != nil {
    rawTotalGB = *client.TotalGBOverride
}

// Стало:
if client.IsTotalGBOverridden {
    rawTotalGB = client.TotalGB
}
```
Аналогично ExpiryTimeOverride.

### 8. `internal/web/service/client_lookup.go`

**resolveEffectiveInboundIds** (строка 132+):
```go
// Было: if client.InboundsOverride != nil || ...// Стало: if client.IsInboundsOverridden
```

### 9. `internal/web/service/client_link.go`

**listForInboundFiltered**:
```go
// Было: WHERE inbounds_override IS NOT NULL
// Стало: WHERE is_inbounds_overridden = TRUE
```

### 10. `internal/web/controller/group.go`

**OverrideField** — вызывает `clientTariffService.OverrideField`, без изменений (сервис берёт на себя).

**resetGroupTariff** — сброс флагов после открепления тарифа:
```go
db.Model(&model.ClientRecord{}).Where("group_name = ?", body.Name).Updates(map[string]any{
    "is_total_gb_overridden":    false,
    "is_limit_ip_overridden":    false,
    "is_expiry_time_overridden": false,
    "is_inbounds_overridden":    false,
    "tariff_started_at":         nil,
})
```

### 11. `internal/web/controller/client.go`

**update** — защита от перезаписи managed-полей при обычном save:
```go
// Было: проверка total_gb_override IS NOT NULL

// Стало: проверка is_total_gb_overridden = TRUE
```

### 12. `internal/web/job/check_client_ip_job.go`

**hasLimitIp**:
```go
// Было: WHERE limit_ip > 0 OR limit_ip_override IS NOT NULL
// Стало: WHERE limit_ip > 0 OR is_limit_ip_overridden = TRUE
```

**loadClientLimits**:
```go
// Было: LimitIPOverride *int
// Стало: IsLimitIPOverridden bool
// effective = isOverridden ? r.LimitIp : tariffLimit ?? r.LimitIp
```

**resolveTariffLimitIPs** — rows-структура меняется, убрать `LimitIpOverride *int`.

### 13. Фронтенд

**`src/schemas/client.ts`** — `ClientRecordSchema`:
```ts
// Удалить: totalGBOverride, limitIPOverride, expiryTimeOverride, inboundsOverride
// Изменить: inboundsOverride → isInboundsOverridden: z.boolean()
// Добавить: totalGBIsOverridden, limitIPIsOverridden, expiryIsOverridden
// Оставить: tariffName, tariffStartedAt
```

**`src/hooks/useTariffOverrides.ts`** — `isFieldManaged` читает `*IsOverridden` из `ClientSlim` (уже bool'и).

**`src/components/ManagedField.tsx`** — без изменений (работает с `managed: boolean`).

**`src/pages/clients/ClientFormModal.tsx`** — `computeDiff`:
```ts
// Было: POST /overrideField { email, field } — бэкенд сам знает effective
// Стало: POST /overrideField { email, field, value } — фронт передаёт новое значение
//         ИЛИ бэкенд сам резолвит effective (как сейчас) — тогда без изменений
```

`OverrideField` на бэкенде уже резолвит effective значение сам, так что фронтенд не меняется.

**`src/pages/clients/ClientsPage.tsx`** — merge order, `*IsOverridden` из `ClientSlim` уже bool.

### 14. Тесты

**`client_tariff_test.go`**:
```go
// Было: TotalGBOverride: int64Ptr(100)
// Стало: TotalGB: 100, IsTotalGBOverridden: true
// Удалить int64Ptr/intPtr хелперы для override
```

**`client_paging.go` SQL-тест** (`TestSqlEffectiveMatchesGoResolver`):
```go
// Setup: убрать TotalGBOverride, ставить IsTotalGBOverridden = true
```

### 15. Удалить

- `int64Ptr`, `intPtr` хелперы из `client_tariff_test.go` (если больше не нужны)
- 3 `*Override` поля из всех структур и запросов

### 16. i18n

Без изменений — ключи `managedFieldLocked`, `makeLocal`, `returnToTariff` остаются.

### Порядок

1. Миграция БД (ручная: ALTER + UPDATE + DROP старых колонок)
2. Модель
3. `client_tariff.go` (резолвер + OverrideField/ReturnToTariff)
4. `client_paging.go` (SQL + ClientSlim)
5. Остальные сервисы
6. Контроллеры
7. Джобы
8. Фронтенд
9. Тесты
