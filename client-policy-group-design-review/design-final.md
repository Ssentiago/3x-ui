# Итоговый дизайн: Client Group Tariffs

**Автор:** DeepSeek V4 Pro
**Предметная область:** дизайн фичи — финальная архитектура
**Ветка:** `feat/client-group-policies`
**Дата:** 2026-07-30

---

## Принятые решения

| Решение | Что выбрано |
|---------|-------------|
| Модель вычислений | **Variant H** — computed view, read-time resolution |
| Сущности | **G+P** — Group + Tariff (Policy переименована) |
| UI-навигация | Единая страница Groups, тарифы встроены как секция |
| Нейминг | Policy → **Tariff / Тариф** |

---

## Модель данных

### Таблицы

```sql
tariffs:
  id, name, total_gb, expiry_days, limit_ip, inbound_ids, enable, created_at, updated_at

client_groups:
  id, name, tariff_id INTEGER NULL REFERENCES tariffs(id), reset_up, reset_down, created_at, updated_at

clients:
  id, email, uuid, enable, group_name, ...
  tariff_id              INTEGER NULL REFERENCES tariffs(id)
  total_gb_override      BIGINT NULL      -- NULL → из тарифа
  limit_ip_override      INTEGER NULL     -- NULL → из тарифа
  expiry_time_override   BIGINT NULL      -- NULL → из тарифа
  comment, tg_id, created_at, updated_at

client_inbounds:
  client_id, inbound_id, flow_override
```

### Правило вычисления (единое, везде)

```
effective(x) = override ?? tariff.x ?? default(0/∞)
```

Nullable override-колонка сама по себе флаг. Никаких JSON-флагов.

### Что уходит

- `inherited_from_policy` (JSON-колонка) — полностью удаляется
- `InheritedFields` — файл `inherited_fields.go` удаляется
- `AutoApplyPolicyToGroups` — не нужен при computed view
- `ApplyPolicyToClient` — не нужен при computed view
- `/policies/:id/apply` — эндпоинт убирается

---

## API-эндпоинты

| Эндпоинт | Поведение |
|----------|-----------|
| `GET /clients/list` | LEFT JOIN tariffs → computed в Go |
| `GET /clients/:email` | LEFT JOIN tariff → computed в Go |
| `POST /clients/add` | override = NULL. Если группа → tariff_id из группы |
| `POST /clients/update/:id` | При смене группы → tariff_id из группы. Override-поля НЕ трогать |
| `POST /clients/del/:id` | Без специальной логики |
| `POST /tariffs/create` | Только INSERT |
| `POST /tariffs/:id/update` | Только UPDATE. **Никакого batch-update clients** |
| `POST /tariffs/:id/delete` | DELETE + `UPDATE clients SET tariff_id = NULL`. Override **не сбрасывать** |
| `POST /groups/create` | Создать с tariff_id |
| `POST /groups/rename` | При смене tariff_id → UPDATE clients.tariff_id. Override НЕ трогать |
| `POST /groups/delete` | Обнулить tariff_id у клиентов. Override НЕ сбрасывать |
| `POST /groups/resetTariff` | Обнулить tariff_id у клиентов. Override НЕ сбрасывать |
| `POST /groups/bulkAdd` | tariff_id = tariff_id группы. Override = NULL |
| `POST /groups/bulkRemove` | Обнулить tariff_id. Override НЕ трогать |
| `POST /overrideField` | `UPDATE SET {field}_override = effective` |
| `POST /returnToTariff` | `UPDATE SET {field}_override = NULL` |

---

## Пользовательские сценарии

### 1. Создание тарифа

```
Админ → Groups → Tariffs section → [+ New Tariff]
  Name: Gold, Traffic: 100 GB, Expiry: 30 days, IP limit: 3, Inbounds: [vmess-us, vless-eu]

INSERT INTO tariffs (name, total_gb, expiry_days, limit_ip, inbound_ids)
VALUES ('Gold', 100, 30, 3, '[1,2]')

Сообщение: «Тариф Gold создан. Назначьте его группе.»
```

### 2. Назначение тарифа группе

```
Groups → дропдаун Tariff в строке «premium» → Gold

UPDATE client_groups SET tariff_id = 42 WHERE name = 'premium'
UPDATE clients SET tariff_id = 42 WHERE group_name = 'premium'
-- override = NULL для новых клиентов группы

Сообщение: «Тариф Gold назначен группе premium (37 клиентов).»
```

### 3. Клиент под тарифом

```
a@b.com: tariff_id = 42 (Gold), все override = NULL

SQL: SELECT c.*, t.total_gb AS tariff_total_gb, ...
     FROM clients c LEFT JOIN tariffs t ON t.id = c.tariff_id

effective_totalGB = NULL ?? 100 = 100
effective_limitIP = NULL ?? 3   = 3
effective_expiry   = NULL ?? now + 30d

Таблица: | a@b.com | 100 | 3 | 2026-08-29 | Gold |
```

### 4. Override поля

```
Clients → Edit a@b.com
  IP limit: [3]  ◎ Inherited (Gold)  [Override]
  Нажимает Override → вводит 5 → Save

UPDATE clients SET limit_ip_override = 5 WHERE email = 'a@b.com'

effective_limitIP = 5 ?? 3 = 5
effective_totalGB = NULL ?? 100 = 100 (из тарифа)

Таблица: | a@b.com | 100 | 5 (✎) | 2026-08-29 | Gold |
```

### 5. Return to tariff

```
Clients → Edit a@b.com
  IP limit: [5]  ◉ Custom  [Return to Gold]
  Нажимает Return to Gold

UPDATE clients SET limit_ip_override = NULL WHERE email = 'a@b.com'

effective_limitIP = NULL ?? 3 = 3
```

### 6. Изменение тарифа — мгновенно, без batch-update

```
Groups → Tariffs → Edit Gold → Traffic: 200 GB → Save

UPDATE tariffs SET total_gb = 200 WHERE id = 42
-- Больше ничего

Следующий запрос списка клиентов:
| a@b.com | 200 | 5 (✎) | 2026-08-29 | Gold |  ← 200 из тарифа, 5 из override
| c@d.com | 200 | 3     | 2026-08-29 | Gold |  ← всё из тарифа
```

### 7. Удаление тарифа

```
Groups → Tariffs → Delete Gold

BEGIN
  UPDATE clients SET tariff_id = NULL WHERE tariff_id = 42
  DELETE FROM tariffs WHERE id = 42
COMMIT
-- override НЕ сбрасываются

Было:  tariff=Gold, limit_ip_override=5    → tariff=NULL, effective_limitIP=5
Было:  tariff=Gold, total_gb_override=NULL → tariff=NULL, effective_totalGB=0 (∞)
```

### 8. Снятие тарифа с группы

```
Groups → строкa «premium» → дропдаун Tariff → «No tariff»

UPDATE clients SET tariff_id = NULL WHERE group_name = 'premium'
UPDATE client_groups SET tariff_id = NULL WHERE name = 'premium'
-- override НЕ трогаются

Сообщение: «Клиенты сохранят индивидуальные настройки.
           Остальные поля вернутся к значениям по умолчанию.»
```

---

## UI: единая страница Groups

```
┌─ Sidebar ─┐  ┌─ Groups ──────────────────────────────────────────┐
│            │  │                              [+ New Group]        │
│  Groups    │  │ ┌────────────────────────────────────────────────┐│
│            │  │ │ Name    │ Clients │ Tariff │ Traffic │ Actions ││
│            │  │ │─────────│─────────│────────│─────────│─────────││
│            │  │ │ premium │   37    │ Gold ▾ │ 1.2 TB  │   ⋯    ││
│            │  │ │ basic   │  120    │ Trial▾ │ 3.4 TB  │   ⋯    ││
│            │  │ └────────────────────────────────────────────────┘│
│            │  │                                                    │
│            │  │ Tariffs                        [+ New Tariff]      │
│            │  │ ┌────────────────────────────────────────────────┐│
│            │  │ │ Name  │ GB  │ Days │ IPs │ Inbounds │ Groups  ││
│            │  │ │───────│─────│──────│─────│──────────│─────────││
│            │  │ │ Gold  │ 100 │  30  │  3  │ 2 inb.   │   1     ││
│            │  │ │ Trial │   5 │   7  │  1  │ 1 inb.   │   2     ││
│            │  │ └────────────────────────────────────────────────┘│
└────────────┘  └────────────────────────────────────────────────────┘
```

- Верхняя таблица — группы. Tariff — inline дропдаун.
- Нижняя таблица — тарифы. Groups — кликабельное число.
- Одна модалка для create/edit тарифа.
- При смене тарифа у группы — confirm с числом клиентов.

---

## Что переименовывается

| Было | Стало |
|------|-------|
| `model.Policy` | `model.Tariff` |
| `policies` (таблица) | `tariffs` |
| `policy_id` (колонка) | `tariff_id` |
| `PolicyService` | `TariffService` |
| `ClientPolicyService` | `ClientTariffService` |
| `inherited_from_policy` (колонка) | **удаляется** |
| `InheritedFields` | **удаляется** |
| `/policies/*` (роуты) | `/tariffs/*` |
| `overrideField` → `returnToPolicy` | `overrideField` → `returnToTariff` |
| `pages.policies.*` (i18n) | `pages.tariffs.*` |
| `PoliciesPage.tsx` | **уходит** (встроено в GroupsPage) |

---

## Что меняется в коде

### Удаляется
- `internal/web/service/inherited_fields.go`
- `client.InheritedFromPolicy` поле модели
- `AutoApplyPolicyToGroups` метод
- `ApplyPolicyToClient` метод в текущем виде
- `POST /policies/:id/apply` эндпоинт
- `PoliciesPage.tsx` (фронтенд)
- Policies роут в сайдбаре
- Миграция: `ALTER TABLE clients DROP COLUMN inherited_from_policy`

### Добавляется
- Три nullable колонки в clients: `total_gb_override`, `limit_ip_override`, `expiry_time_override`
- `tariff_id` в clients (FK на tariffs)
- `tariff_id` в client_groups (FK на tariffs)
- LEFT JOIN tariffs в список/детали клиентов
- Computed-методы: `EffectiveTotalGB`, `EffectiveLimitIP`, `EffectiveExpiryTime`
- Поля в API-ответе: `tariffId`, `tariffName`, `totalGBIsOverridden`, `limitIPIsOverridden`, `expiryIsOverridden`
- `applyInboundList` при AddToGroup (баг P0 из аудита)

### Изменяется
- `OverrideField`: `UPDATE col_override = current_effective`
- `ReturnToTariff`: `UPDATE col_override = NULL`
- `UpdateTariff`: **убирается** массовый apply
- `AddToGroup`: сбрасывает override в NULL
- `DeleteTariff`: обнуляет tariff_id у клиентов, override сохраняет
- Xray config generation: computed вместо client.TotalGB

---

## Фронтенд

### Новая структура

```
frontend/src/pages/groups/
  GroupsPage.tsx            ← единая страница: две таблицы
  GroupTable.tsx            ← верхняя таблица групп
  TariffTable.tsx           ← нижняя таблица тарифов
  TariffFormModal.tsx       ← модалка create/edit тарифа
  GroupFormModal.tsx        ← модалка create/edit группы
  GroupAddClientsModal.tsx  ← без изменений
  GroupRemoveClientsModal.tsx ← без изменений
```

### Индикаторы override в форме клиента

```tsx
// Каждое поле:
<InputNumber value={effective} disabled={!overridden} />
{tariffName ? (
  overridden ? (
    <Tag color="orange">◉ Custom <Button>Return to {tariffName}</Button></Tag>
  ) : (
    <Tag color="green">◎ {tariffName} <Button>Override</Button></Tag>
  )
) : (
  <Tag>Manual</Tag>
)}
```

---

## Сравнение с текущей реализацией

| Операция | Текущий (A) | Итоговый (H, G+P, Tariff) |
|----------|-------------|---------------------------|
| Изменить тариф | batch-update всех клиентов | UPDATE одной строки tariffs |
| Override поля | parse JSON → modify → save JSON | `UPDATE clients SET col = x` |
| Return к тарифу | parse JSON → modify → save JSON | `UPDATE clients SET col = NULL` |
| Снять тариф с группы | очистить JSON-флаги у всех | `UPDATE clients SET tariff_id = NULL` |
| Список клиентов | 1 запрос | 1 запрос + LEFT JOIN |
| Удалить тариф | блокировка если есть группы | разрешено, override сохраняются |
| Сайдбар | Groups → подменю (Groups + Policies) | Groups (плоский пункт) |
| Страниц | 2 | 1 |
