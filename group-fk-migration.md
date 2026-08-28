# FK migration: clients.group_name → clients.group_id

## Problem

Группы и клиенты связаны строковым джойном: `clients.group_name = client_groups.name`.
Никакого FOREIGN KEY, никакой ссылочной целостности.

С появлением тарифов (`client_groups.tariff_id`) строка-ключ стала узким местом:
переименование группы требует UPDATE десятков/сотен/тысяч строк клиентов.

При удалении группы без каскада клиенты остаются с битым `group_name`,
который ссылается на несуществующую строку.

## Что предлагается

Перейти на `clients.group_id INT REFERENCES client_groups(id)`.

```
                БЫЛО                              СТАНЕТ
  clients.group_name = 'VIP'          clients.group_id = 42  ← FK
          ↓ строковый join                     ↓
  client_groups.name = 'VIP'          client_groups.id = 42
                                        client_groups.name = 'VIP'  ← просто label
```

## Миграция

### 1. Схема

```sql
-- Добавить колонку (nullable, временно)
ALTER TABLE clients ADD COLUMN group_id INTEGER
  REFERENCES client_groups(id) ON DELETE SET NULL;

-- Заполнить из существующих group_name
UPDATE clients
SET group_id = (SELECT id FROM client_groups WHERE name = clients.group_name)
WHERE group_name != '';

-- Индекс для join'ов
CREATE INDEX idx_clients_group_id ON clients(group_id);
```

Старая колонка `clients.group_name` остаётся: можем продолжать писать в неё для
обратной совместимости, а запросы постепенно перевести на `group_id`. После
следующего мажорного релиза — удалить.

### 2. Модель

```go
// ClientRecord
GroupID  *int   `json:"groupId" gorm:"column:group_id;index"`          // FK to client_groups.id
Group    string `json:"group" gorm:"column:group_name;default:''"`     // deprecated, keep for backcompat

// GroupSummary — возвращается фронтенду
type GroupSummary struct {
    ID       int    `json:"id"`
    Name     string `json:"name"`
    // ...
}
```

## Изменения в коде

### 1. Переименование группы — одна строка

```go
// Было: replaceGroupValue(oldName, newName)
//   1. UPDATE client_groups SET name = ?
//   2. UPDATE clients SET group_name = ? WHERE group_name = ?
//   3. UPDATE inbound.settings JSON (уже удалено в pr/72e04cf)

// Стало:
func RenameGroup(id int, newName string) error {
    return db.Model(&ClientGroup{}).Where("id = ?", id).Update("name", newName).Error
}
```

Никаких клиентов не трогаем. `affected` не нужен — ID не изменился.

### 2. Удаление группы

```go
// Было: DeleteGroup(name) → DELETE client_groups + UPDATE clients.group_name = ''

// Стало:
func DeleteGroup(id int) error {
    // FK ON DELETE SET NULL сам обнулит clients.group_id
    return db.Where("id = ?", id).Delete(&ClientGroup{}).Error
}
```

### 3. Добавление клиента в группу

```go
// Было:
//   tx.UpdateColumn("group_name", group)

// Стало:
//   tx.UpdateColumn("group_name", group)         // обратная совместимость
//   tx.UpdateColumn("group_id", groupRecord.ID)  // FK
```

### 4. Запросы — везде `group_id` вместо `group_name`

```go
// Было:
db.Where("group_name = ?", groupName).Find(&records)
db.Where("group_name = ?", body.NewName).Count(&affected)

// Стало:
db.Where("group_id = ?", groupId).Find(&records)
db.Where("group_id = ?", groupId).Count(&affected)
```

Файлы, которые надо обновить:
- `internal/web/service/client_groups.go` — все group-операции
- `internal/web/service/client_paging.go` — фильтры/сортировка по группе
- `internal/web/service/client_link.go` — `tariffIdsContainingInbound` (джойн к `client_groups` уже по `id`, но `listForInboundFiltered` фильтрует по group)
- `internal/web/service/client_lookup.go`
- `internal/web/service/client_tariff.go` — `resolveForClient` (джойн к `client_groups`)
- `internal/web/controller/group.go` — параметры эндпоинтов
- `internal/web/job/check_client_ip_job.go` — `hasLimitIp`, `loadClientLimits`, `resolveTariffLimitIPs`

### 5. Фронтенд

```typescript
// GroupSummary — теперь с id
interface GroupSummary {
    id: number;          // ← новое
    name: string;
    tariffId?: number;
    clientCount: number;
}

// Фильтр по группе: clients?groupId=X вместо clients?group=X
// Клиент при создании/редактировании: select по group_id, не по name
// GroupForm в модалках: value = group.id, label = group.name
```

Файлы:
- `frontend/src/schemas/client.ts` — `GroupSummarySchema`
- `frontend/src/pages/groups/GroupsTab.tsx` — rename/delete/action прокидывают `id`
- `frontend/src/pages/groups/GroupFormModal.tsx` — селект по `{id, name}`
- `frontend/src/pages/clients/ClientFormModal.tsx` — выбор группы по `groupId`
- `frontend/src/pages/clients/ClientsPage.tsx` — фильтр по `groupId`
- Все bulk-модалки — `groupId` вместо `groupName`

## Эндпоинты

Меняются с name-based на id-based:

| Было | Стало |
|---|---|
| `POST /groups/create {name, tariffId}` | `POST /groups/create {name, tariffId}` — возвращает `{id, name}` |
| `POST /groups/rename {oldName, newName}` | `POST /groups/rename {id, name}` |
| `POST /groups/delete {name}` | `POST /groups/delete {id}` |
| `POST /groups/bulkAdd {emails, group}` | `POST /groups/bulkAdd {emails, groupId}` |
| `POST /groups/bulkRemove {emails}` | `POST /groups/bulkRemove {groupId, emails}` — опциональный groupId для scope'а |
| `GET /groups` | `GET /groups` — теперь возвращает `id` в каждом элементе |

## Порядок работы

1. Миграция БД (добавить колонку + заполнить + индекс)
2. Модель (`ClientRecord.GroupID`, `ClientGroup.ID` в GroupSummary)
3. Сервисный слой (все операции через `group_id`)
4. Контроллеры (id вместо name в параметрах)
5. Фронтенд (GroupSummary с id, везде `groupId`)
6. Тесты
7. Удаление старой колонки `clients.group_name` — следующим мажорным релизом

## Риски

- **Миграция на проде**: UPDATE по `group_name` может затронуть десятки тысяч строк. Нужно тестировать на больших объёмах.
- **Обратная совместимость API**: эндпоинты меняют параметры — старый фронтенд/клиенты сломаются. Придётся синхронизировать деплой.
- **Node sync**: нужно убедиться что `SyncInbound` корректно работает с `group_id`.
