# Design: Group / Tariff / Profile

**Ветка:** `feat/client-group-tariffs`
**Дата:** 2026-07-31
**Статус:** принято, нужна спецификация

---

## Три сущности

### Group
- Организационная метка на клиенте. Не хранит настроек.
- Назначение: фильтрация, сегментация, привязка к биллингу.
- Хранит `tariff_id` — ссылку на один тариф.

### Tariff
- Именованная композиция: упорядоченный список профилей + стратегии наложения для traffic и inbounds.
- Сам значений не хранит — только ссылки на профили и их порядок.
- Порядок важен: профили накладываются последовательно, поздний перезаписывает/суммирует более ранний.
- Один тариф может быть привязан к нескольким группам.

### Profile
- Базовый кирпич настроек. Хранит конкретные значения полей.
- Переиспользуется: один профиль может входить в несколько тарифов на разных позициях.
- Порядок — свойство связи Tariff↔Profile, не самого профиля.

---

## Модель данных

```sql
profiles:
  id, name (UNIQUE), traffic (INTEGER NULL), expiry_days (INTEGER NULL),
  limit_ip (INTEGER NULL), inbound_ids (TEXT DEFAULT '[]' NULL),
  created_at, updated_at

tariffs:
  id, name (UNIQUE),
  traffic_strategy (TEXT NOT NULL DEFAULT 'overwrite'),
  inbound_strategy (TEXT NOT NULL DEFAULT 'overwrite'),
  created_at, updated_at

tariff_profiles:
  tariff_id (FK → tariffs), profile_id (FK → profiles), position (INTEGER),
  PRIMARY KEY (tariff_id, profile_id), UNIQUE (tariff_id, position)

client_groups:
  id, name (UNIQUE), tariff_id (INTEGER NULL FK → tariffs), reset_up, reset_down,
  created_at, updated_at

clients:
  id, email, uuid, enable, group_name,
  total_gb_override (BIGINT NULL), limit_ip_override (INTEGER NULL),
  expiry_time_override (BIGINT NULL), inbounds_override (INTEGER NULL),
  tariff_started_at (BIGINT NULL),
  comment, tg_id, created_at, updated_at
```

Старые колонки `tariffs.total_gb, expiry_days, limit_ip, inbound_ids` — исключаются из Go-модели (GORM их проигнорирует, в БД останутся но не читаются).

### Nullable-семантика полей профиля

- `traffic IS NULL` — профиль не высказывается про трафик, пропускается при наложении
- `traffic = 0` — явно unlimited
- `traffic = 100` — 100 GB

Аналогично `expiry_days`, `limit_ip`, `inbound_ids`. NULL ≠ 0.

### Стратегии наложения (на уровне тарифа)

| Поле | Стратегии |
|------|-----------|
| traffic | `overwrite` — последний non-null заменяет; `sum` — сумма всех non-null |
| expiry_days | всегда `overwrite` — последний non-null заменяет |
| limit_ip | всегда `overwrite` — последний non-null заменяет |
| inbound_ids | `overwrite` — последний non-null заменяет; `union` — объединение всех non-null списков |

---

## Уровни резолвинга

```
client.override ?? tariff_chain_resolved ?? default(0 = unlimited)
```

### Алгоритм

```
Для клиента C с группой G (у которой тариф T):

1. Собираем profile_chain = T.profiles по position ASC (0..N)

2. Для каждого поля field:
   effective = default(field)  // 0 = unlimited/never

   Для каждого профиль P в profile_chain:
     val = P.field
     если val IS NULL → skip
     иначе:
       strategy = T.{field}_strategy
       если strategy == overwrite: effective = val
       если strategy == sum:       effective += val

3. Финальное значение:
   traffic  = C.totalGBOverride  ?? effective_traffic  ?? C.totalGB
   limit_ip = C.limitIPOverride  ?? effective_ip       ?? C.limitIP
   expiry   = C.expiryTimeOverride ?? (C.tariffStartedAt + effective_days*86400*1000) ?? C.expiryTime
   inbounds = C.inboundsOverride != null ? C.inboundIds : (effective_inbounds ?? C.inboundIds)
```

### Inbounds: детали

- `overwrite`: берём inbound_ids последнего профиля с non-null значением
- `union`: собираем все non-null inbound_ids, дедуплицируем, возвращаем единый список
- `restrict`: пересекаем все non-null inbound_ids; если хотя бы один список пуст после пересечения — результат пуст (клиент без inbound-ов)

### Traffic: единицы

Profile хранит traffic в **GB** (int64). Эффективное значение вычисляется в **bytes** (`<< 30`). На входе и выходе резолвера — bytes.

---

## API

### Profile

| Метод | Путь | Что |
|-------|------|-----|
| `GET` | `/profiles` | Список всех профилей |
| `GET` | `/profiles/:id` | Один профиль |
| `POST` | `/profiles/create` | `{ name, traffic?, expiryDays?, limitIp?, inboundIds? }` |
| `POST` | `/profiles/:id/update` | Обновить поля |
| `POST` | `/profiles/:id/delete` | Блокировать если используется в tariff_profiles, с перечнем тарифов |

### Tariff

| Метод | Путь | Что |
|-------|------|-----|
| `GET` | `/tariffs` | Список с groupCount и clientCount |
| `GET` | `/tariffs/:id` | Тариф + профили с порядком + resolved-значения |
| `POST` | `/tariffs/create` | `{ name, trafficStrategy, inboundStrategy }` |
| `POST` | `/tariffs/:id/update` | Обновить name/стратегии → refresh client_traffics для всех клиентов под тарифом |
| `POST` | `/tariffs/:id/delete` | Блокировать если groupCount > 0 |
| `POST` | `/tariffs/:id/profiles` | `{ profileIds: [{id, position}] }` — атомарная замена списка |

`GET /tariffs/:id` ответ:

```json
{
  "id": 1,
  "name": "Gold",
  "trafficStrategy": "sum",
  "inboundStrategy": "union",
  "profiles": [
    { "id": 1, "name": "BASE", "position": 0 },
    { "id": 2, "name": "PRO_ADDON", "position": 1 }
  ],
  "resolved": {
    "traffic": 644245094400,
    "expiryDays": 30,
    "limitIp": 5,
    "inboundIds": [1, 2, 3]
  },
  "groupCount": 3,
  "clientCount": 120
}
```

### Client override — без изменений

`POST /overrideField`, `POST /returnToTariff` — как сейчас.

### Client

`GET /:email` — без изменений (raw-значения). `GET /:email/effective` — новый роут, отдаёт effective-значения.

### Group — без изменений в сигнатурах

Внутренняя логика переезжает на новый резолвер через цепочку профилей.

### Удалённые эндпоинты

- `POST /tariffs/:id/apply` — больше нет, изменение тарифа мгновенно через read-time резолв

---

## UI

### Страница Groups (единая)

```
┌─ Groups ────────────────────────────────────────────────────────┐
│                                                                    │
│  Groups                           [+ New Group]                    │
│  ┌───────────────────────────────────────────────────────────┐    │
│  │ Name      │ Clients │ Tariff  │ Traffic │ Actions          │    │
│  │───────────│─────────│─────────│─────────│──────────────────│    │
│  │ premium   │   37    │ Gold ▾  │ 1.2 TB  │ ⋯                │    │
│  │ basic     │  120    │ Basic▾  │ 3.4 TB  │ ⋯                │    │
│  └───────────────────────────────────────────────────────────┘    │
│                                                                    │
│  ┌─ Profiles ─────────────────────────────────────────────────┐   │
│  │                        [+ New Profile]                       │   │
│  │ ┌──────────────────────────────────────────────────────┐    │   │
│  │ │ Name      │ Traffic │ Expiry │ IPs │ Inbounds │ Used │    │   │
│  │ │───────────│─────────│────────│─────│──────────│──────│    │   │
│  │ │ BASE      │ 100 GB  │ 30 d   │ 3   │ 2 inbs   │ 2    │    │   │
│  │ │ PRO_ADDON │ 500 GB  │ 60 d   │ 5   │ 1 inb    │ 1    │    │   │
│  │ └──────────────────────────────────────────────────────┘    │   │
│  └────────────────────────────────────────────────────────────┘   │
│                                                                    │
│  ┌─ Tariffs ──────────────────────────────────────────────────┐   │
│  │                        [+ New Tariff]                        │   │
│  │ ┌──────────────────────────────────────────────────────┐    │   │
│  │ │ Name  │ Profiles      │ Groups │ Clients │ Actions   │    │   │
│  │ │───────│───────────────│────────│─────────│───────────│    │   │
│  │ │ Gold  │ BASE + PRO    │   3    │  120    │ ⋯         │    │   │
│  │ │ Trial │ TRIAL         │   2    │   50    │ ⋯         │    │   │
│  │ └──────────────────────────────────────────────────────┘    │   │
│  └────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────┘
```

Три вкладки на единой странице Groups: Groups / Profiles / Tariffs.

### Tariff edit modal — живой резолв

```
┌─ Edit Tariff: "Gold" ─────────────────────────────────────────┐
│                                                                  │
│  Profile chain                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ ☰ Profile BASE_TRAFFIC     100GB, 3 IP, vmess+vless     │    │
│  │ ☰ Profile PRO_ADDON        500GB, 5 IP                  │    │
│  │ ☰ Profile TROJAN_ACCESS    trojan inbound               │    │
│  │                                    [+ Add Profile]      │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  Resolved preview                                               │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ Traffic:  600 GB    [sum ▼]      ← 100 + 500              │    │
│  │ Expiry:   60 days                 ← от PRO_ADDON            │    │
│  │ IP limit: 5                       ← от PRO_ADDON            │    │
│  │ Inbounds: [1,2,3]   [union ▼]    ← vmess+vless+trojan     │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│                                         [Save]                   │
└──────────────────────────────────────────────────────────────────┘
```

- Drag-and-drop переупорядочивание профилей
- Дропдауны стратегий прямо на полях мини-профиля
- Стендовый мини-профиль пересчитывается live при любом изменении порядка, состава профилей, или стратегий

---

## Структура кода

### Новые файлы

```
internal/database/model/profile.go
internal/web/service/profile.go
internal/web/controller/profile.go
frontend/src/schemas/profile.ts
frontend/src/pages/groups/ProfileTab.tsx
```

### Изменяемые файлы

```
internal/database/model/model.go        Tariff — убрать поля значений, добавить стратегии
internal/database/db.go                 AutoMigrate новые таблицы (без data-миграции)
internal/web/service/tariff.go          CRUD + Resolve(tariff) → EffectiveConfig
internal/web/service/client_tariff.go   Резолвер: override → chain → default
internal/web/service/client_paging.go   SQL effective-выражения через JOIN tariff_profiles + profiles
internal/web/controller/group.go        Роуты профилей, обновлённая логика тарифов
internal/web/controller/client.go       GET /:email/effective — новый роут, GET /:email без изменений
internal/web/job/check_client_ip_job.go IP-limit через новый резолвер
internal/web/service/inbound_traffic.go Обновить resolveEffectiveTraffic
frontend/src/pages/groups/GroupsPage.tsx Три вкладки, модалка тарифа с живым резолвом
frontend/src/pages/clients/ClientFormModal.tsx  Без изменений
frontend/src/pages/api-docs/endpoints.ts       Новые роуты профилей
frontend/src/generated/                        Регенерация через make gen
tools/openapigen/main.go                       Profile, TariffProfile в allowlist
```

### Удаляемое

Из Go-модели `Tariff`: поля `TotalGB, ExpiryDays, LimitIP, InboundIds`. В БД колонки остаются, GORM их игнорирует.

### Dead code к удалению (из текущей ветки)

```
client_tariff.go: IsOverriddenTotalGB, IsOverriddenLimitIP, IsOverriddenExpiry,
                 ApplyTariffToNewClient
group.go:        convertTariffSummaryToModel
```

---

## Что уходит из текущей реализации

- `Tariff.TotalGB / ExpiryDays / LimitIP / InboundIds` — больше не на тарифе
- `syncTariffInbounds` — заменяется резолвером inbound-ов через цепочку профилей
- `POST /tariffs/:id/apply` — не нужен при read-time резолве
- `ApplyPolicyToClient`, `AutoApplyPolicyToGroups` — уже удалены в предыдущем рефакторинге

## Что остаётся без изменений

- Override-колонки на клиенте и их API
- `tariff_started_at` на клиенте
- Группы и их связи с клиентами
- Клиентская форма, ManagedField, useTariffOverrides
- IP-limit job, traffic accounting, subscription links — только резолвер меняется внутри
