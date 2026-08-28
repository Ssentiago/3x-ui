# Client Group Policies — Вариант H: Computed View (read-time resolution)

**Автор:** DeepSeek V4 Pro
**Предметная область:** дизайн фичи — целевая архитектура
**Ветка:** `feat/client-group-policies`
**Дата:** 2026-07-30

---

## 1. Модель данных

### Таблицы

```sql
clients:
  id, email, uuid, enable, group_name, ...
  policy_id            INTEGER NULL REFERENCES policies(id)
  total_gb_override    BIGINT NULL      -- NULL → из политики
  limit_ip_override    INTEGER NULL     -- NULL → из политики
  expiry_time_override BIGINT NULL      -- NULL → из политики
  comment, tg_id, created_at, updated_at

policies:
  id, name, total_gb, expiry_days, limit_ip, inbound_ids, enable, created_at, updated_at

client_groups:
  id, name, policy_id, reset_up, reset_down, created_at, updated_at

client_inbounds:
  client_id, inbound_id, flow_override
```

### Что уходит

- `inherited_from_policy` (JSON-колонка в clients) — **полностью удаляется**
- `InheritedFields` (структура, парсер, маршалер) — **весь файл** `inherited_fields.go` уходит
- `ClientPolicyService` — радикально упрощается, остаётся только inbound apply

### Правило вычисления (единое, везде)

```
effective(x) = override ?? if policy exists then policy.x else x_default
```

Никаких флагов. Nullable override-колонка сама по себе флаг.

---

## 2. Полный перечень API-эндпоинтов и их поведение

| Эндпоинт | Что делает с override-полями |
|----------|------------------------------|
| `GET /clients/list` | LEFT JOIN policies → computed в Go |
| `GET /clients/:email` | LEFT JOIN policy → computed в Go |
| `POST /clients/add` | При создании: override = NULL. Если указана группа с policy → `policy_id` выставляется |
| `POST /clients/update/:id` | Если изменилась группа → обновить `policy_id` (взять из группы). Override-поля НЕ трогать |
| `POST /clients/del/:id` | Ничего специального |
| `POST /policies/create` | Только INSERT в policies |
| `POST /policies/:id/update` | Только UPDATE policies. **Никакого batch-update clients** |
| `POST /policies/:id/delete` | DELETE из policies. Выставить `policy_id = NULL` клиентам в затронутых группах. Override-поля **не сбрасывать** |
| `POST /policies/:id/apply` | **Убрать.** Не нужен — политика действует через read-time compute |
| `POST /groups/create` | При создании с `policyId` → группа получает `policy_id` |
| `POST /groups/rename` | При смене `policyId` в rename → обновить `policy_id` у всех клиентов группы. Override-поля НЕ трогать |
| `POST /groups/delete` | Обнулить `policy_id` у клиентов группы. Override-поля **не сбрасывать** |
| `POST /groups/resetPolicy` | Обнулить `policy_id` у клиентов группы. Override-поля **не сбрасывать** |
| `POST /groups/bulkAdd` | Выставить `policy_id` у добавляемых клиентов = `policy_id` группы. Override = NULL |
| `POST /groups/bulkRemove` | Обнулить `policy_id` у удаляемых. Override-поля НЕ трогать |
| `POST /overrideField` | `UPDATE clients SET {field}_override = <текущее_effective_значение>` |
| `POST /returnToPolicy` | `UPDATE clients SET {field}_override = NULL` |

---

## 3. Пользовательские сценарии — по шагам

### Сценарий 1: Создание политики

```
Админ → Policies → Create «Gold»: totalGB=100, expiryDays=30, limitIP=3, inboundIds=[1,2]

Система: INSERT INTO policies (name, total_gb, expiry_days, limit_ip, inbound_ids)
         VALUES ('Gold', 100, 30, 3, '[1,2]')

Состояние: политика создана. Никакие клиенты не затронуты.
Сообщение: «Gold создана. Назначьте её группе клиентов.»
```

### Сценарий 2: Назначение политики группе

```
Админ → Groups → Edit group «premium» → Policy: «Gold» → Save

Система: BEGIN
         UPDATE client_groups SET policy_id = 42 WHERE name = 'premium'
         UPDATE clients SET policy_id = 42 WHERE group_name = 'premium'
         COMMIT
         Override-поля клиентов НЕ трогаются.

Сообщение: «Политика Gold назначена группе premium (12 клиентов).»
```

### Сценарий 3: Клиент попадает под политику — как выглядят effective-значения

```
Исходное состояние клиента a@b.com:
  total_gb_override = NULL
  limit_ip_override = NULL
  expiry_time_override = NULL
  policy_id = 42 (Gold)

Gold.total_gb = 100, Gold.expiry_days = 30, Gold.limit_ip = 3

SQL для списка:
  SELECT c.*, p.total_gb AS policy_total_gb, p.limit_ip AS policy_limit_ip, p.expiry_days AS policy_expiry_days
  FROM clients c LEFT JOIN policies p ON p.id = c.policy_id

Computed:
  effective_totalGB  = NULL ?? 100 = 100
  effective_limitIP  = NULL ?? 3   = 3
  effective_expiry   = NULL ?? now + 30d = 2026-08-29

Клиент в таблице:
| Email    | Total GB | Limit IP | Expiry      | Policy |
|----------|----------|----------|-------------|--------|
| a@b.com  | 100      | 3        | 2026-08-29  | Gold   |
```

### Сценарий 4: Override одного поля

```
Админ → Clients → Edit a@b.com

Видит:
  Traffic limit: [100] GB   ◎ Inherited (Gold)  [Override]
  IP limit:      [3]        ◎ Inherited (Gold)  [Override]

Нажимает [Override] на IP limit → поле разблокируется → вводит 5 → Save.

Система: UPDATE clients SET limit_ip_override = 5 WHERE email = 'a@b.com'

Теперь:
  effective_limitIP = 5 ?? 3 = 5  (override)
  effective_totalGB = NULL ?? 100 = 100  (из политики)

Клиент в таблице:
| Email    | Total GB | Limit IP | Expiry      | Policy |
|----------|----------|----------|-------------|--------|
| a@b.com  | 100      | 5 (✎)    | 2026-08-29  | Gold   |
```

### Сценарий 5: Return to policy

```
Админ → Clients → Edit a@b.com
Видит:
  IP limit: [5] IPs  ◉ Custom  [Return to Gold]

Нажимает [Return to Gold] → поле блокируется, показывает 3.

Система: UPDATE clients SET limit_ip_override = NULL WHERE email = 'a@b.com'

Теперь effective_limitIP = NULL ?? 3 = 3  (снова из политики)
```

### Сценарий 6: Изменение политики — магия без batch-update

```
Админ → Policies → Edit «Gold» → totalGB = 200 → Save

Система: UPDATE policies SET total_gb = 200 WHERE id = 42
         Больше НИЧЕГО.

Немедленный эффект при следующем запросе списка клиентов:
| Email    | Total GB | Limit IP | Expiry      | Policy |
|----------|----------|----------|-------------|--------|
| a@b.com  | 200      | 5 (✎)    | 2026-08-29  | Gold   |  ← 200 из политики, 5 из override
| c@d.com  | 200      | 3        | 2026-08-29  | Gold   |  ← все из политики

Никаких batch-update. Никаких транзакций на сотнях записей.
```

### Сценарий 7: Удаление политики

```
Админ → Policies → Delete «Gold»

Система: BEGIN
         UPDATE clients SET policy_id = NULL WHERE policy_id = 42
         DELETE FROM policies WHERE id = 42
         COMMIT
         Override-поля НЕ сбрасываются.

Теперь у клиентов:
  policy_id = NULL
  total_gb_override = NULL  (или не-NULL если был override)
  limit_ip_override = 5     (сохранился override!)

Effective значения:
  totalGB: NULL ?? (нет политики) = 0  → ∞ (клиент без лимита)
  limitIP: 5 ?? (нет политики) = 5     → override уцелел
```

Это осознанное поведение: override-значения — волеизъявление админа. Если админ сказал «этому клиенту лимит IP = 5», то при снятии политики лимит IP остаётся 5. Если override не было — клиент возвращается к дефолту (0 = безлимит).

### Сценарий 8: Снятие политики с группы

```
Админ → Groups → Remove Policy у группы «premium»

Система: UPDATE clients SET policy_id = NULL WHERE group_name = 'premium'
         UPDATE client_groups SET policy_id = NULL WHERE name = 'premium'
         Override-поля НЕ трогаются.

Сообщение: «Клиенты сохранят индивидуальные настройки (overrides).
           Остальные поля вернутся к значениям по умолчанию.»
```

### Сценарий 9: Клиент без группы и без политики

```
Клиент создан с policy_id = NULL, все override = NULL.

effective_totalGB  = NULL ?? (нет) = 0 → ∞
effective_limitIP  = NULL ?? (нет) = 0 → ∞
effective_expiry   = NULL ?? (нет) = 0 → never

Таблица:
| Email    | Total GB | Limit IP | Expiry | Policy |
|----------|----------|----------|--------|--------|
| x@y.com  | ∞        | ∞        | never  | —      |
```

### Сценарий 10: Клиент с ручными значениями без политики

```
Админ → Clients → Add, вводит totalGB=50, limitIP=2

Вариант H2 (единые nullable override-поля):
  INSERT INTO clients (email, total_gb_override=50, limit_ip_override=2, policy_id=NULL)

  effective_totalGB = 50 ?? (нет) = 50
  effective_limitIP = 2 ?? (нет) = 2

  Нет разницы между «override поверх политики» и «ручной ввод без политики».
  50 = 50 в любом случае.

Вариант H1 (две пары колонок):
  total_gb (ручное) + total_gb_override (поверх политики)
  effective = total_gb_override ?? policy.total_gb ?? total_gb

  H2 проще. Рекомендован H2.
```

### Сценарий 11: Добавление клиента в группу с политикой

```
Админ → Clients → bulkAdd → группа «premium» (policy Gold)

Система:
  UPDATE clients SET group_name = 'premium' WHERE email IN (...)
  UPDATE clients SET policy_id = 42 WHERE email IN (...)
  Override-поля = NULL  ← клиент начинает с чистого наследования
```

### Сценарий 12: Создание нового клиента с выбором группы

```
Админ → Clients → Add
  Email: new@b.com
  Group: premium (имеет policy Gold)
  Остальные поля не трогает

Система:
  INSERT INTO clients (email, group_name='premium', policy_id=42,
    total_gb_override=NULL, limit_ip_override=NULL, expiry_time_override=NULL)

После создания effective сразу = значения из Gold.
```

### Сценарий 13: Xray конфиг

```
При старте/рестарте Xray:
  1. Загружаем все client_inbounds
  2. Загружаем всех clients с LEFT JOIN policies
  3. Для каждого клиента:
     totalGB   = effectiveTotalGB(client, policyMap)
     expiryMs  = effectiveExpiryTime(client, policyMap)
     limitIP   = effectiveLimitIP(client, policyMap)
  4. Генерируем Xray JSON

При изменении политики:
  SetToNeedRestart() — да, перегенерировать конфиг нужно (inbound-ы могли измениться)
  Но данные клиентов в БД не меняются
```

---

## 4. Полная таблица: что видит админ после каждого действия

| Действие | effective-значения клиентов | override-поля | БД-изменения |
|----------|----------------------------|---------------|--------------|
| Создать политику | Не меняются | Не меняются | INSERT policies |
| Изменить политику | **Меняются мгновенно** на след. запросе | Не меняются | UPDATE policies |
| Назначить политику группе | **Меняются** для клиентов группы | Сбрасываются в NULL | UPDATE clients SET policy_id |
| Снять политику с группы | Возвращаются к дефолтам (0/∞/never) | **Сохраняются** | UPDATE clients SET policy_id=NULL |
| Удалить политику | Возвращаются к дефолтам | **Сохраняются** | DELETE policies, UPDATE clients |
| Override поля | Меняется только это поле | Пишется значение | UPDATE clients SET col_override |
| Return to policy | Возвращается к значению из policy | Становится NULL | UPDATE clients SET col_override=NULL |
| Добавить клиента в группу | Применяются значения политики | Сбрасываются в NULL | UPDATE clients |

---

## 5. Краевые случаи

### Кейс: политика удалена, клиент остался с override

```
Было:  policy=Gold, total_gb_override=50
Стало: policy=NULL, total_gb_override=50
effective: 50

Было:  policy=Gold, limit_ip_override=NULL
Стало: policy=NULL, limit_ip_override=NULL
effective: 0 → ∞
```

Override переживает удаление политики. Отсутствие override = дефолт.

### Кейс: группа без явной записи в client_groups, но с policy_id у клиентов

Группы в этой панели существуют в двух видах: явные (`client_groups`) и неявные (просто `group_name` в clients). Если клиент имеет `group_name='premium'`, а записи в `client_groups` нет, то `policy_id` клиента берётся напрямую, а не из группы.

При `ListGroups`:
- derived из clients.group_name
- stored из client_groups
- merge
- policy берём из client_groups.policy_id, если есть

Если у группы нет явной записи в `client_groups`, но у клиентов `policy_id` выставлен — это ок. Policy применяется на уровне клиента, группа — просто организационный ярлык.

### Кейс: два админа одновременно меняют политику и override

```
Админ A: меняет Gold.total_gb = 200
Админ B: ставит total_gb_override = 150 клиенту a@b.com

Результат:
  A сохранил → effective у всех = 200 (кроме тех у кого override)
  B сохранил → effective у a@b.com = 150 (override приоритетнее)
```

Никаких конфликтов. Они работают с разными строками и разными колонками.

### Кейс: политика с нулевыми значениями

```
Gold: total_gb=0, expiry_days=0, limit_ip=0

Это валидная политика «без ограничений».
effective_totalGB клиента с такой политикой = 0 (безлимит).
Если админ хочет реальный ноль трафика — он ставит total_gb_override=0.
```

---

## 6. Что меняется в коде

### Удаляется полностью

- `internal/web/service/inherited_fields.go` — весь файл
- `client.InheritedFromPolicy` — поле из модели ClientRecord
- `AutoApplyPolicyToGroups` — метод
- `ApplyPolicyToClient` — метод в текущем виде
- `/policies/:id/apply` — эндпоинт
- Миграция: `ALTER TABLE clients DROP COLUMN inherited_from_policy`

### Добавляется

- Три nullable колонки в `clients` (миграция)
- LEFT JOIN policies в список клиентов
- Computed-методы: `EffectiveTotalGB()`, `EffectiveExpiryTime()`, `EffectiveLimitIP()`
- Новые поля в API-ответе: `policyId`, `policyName`, `totalGBIsOverridden`, `limitIPIsOverridden`, `expiryIsOverridden`
- Новый `applyInboundList` при `AddToGroup` (баг P0)

### Изменяется

- `OverrideField`: вместо JSON-манипуляции → `UPDATE col_override = current_effective`
- `ReturnToPolicy`: вместо JSON-манипуляции → `UPDATE col_override = NULL`
- `UpdatePolicy`: **убирается** AutoApplyPolicyToGroups
- `AddToGroup`: сбрасывает override-поля в NULL
- `DeletePolicy`: обнуляет `policy_id` у клиентов, не трогает override
- Xray config generation: computed вместо чтения client.TotalGB

---

## 7. Фронтенд — что меняется

### Уходит

- Баннер «Managed fields» с перечислением — заменяется на inline-индикаторы у каждого поля
- Логика disabled/readonly для inherited-полей — заменяется на inline Override/Return
- Два вызова при save policy (create + apply) → один вызов (create)

### Появляется

- В строке клиента — иконка ✎ на override-значениях
- В форме клиента — индикатор `◎ Inherited (Gold)` / `◉ Custom` у каждого поля
- Кнопка `[Override]` / `[Return to Gold]` у каждого поля
- Колонка Policy в таблице клиентов: зелёный тег с именем или прочерк
- Tooltip на колонке «Used by groups»: список имён групп

### Деталь формы клиента

```tsx
// Псевдокомпонент поля
function PolicyField({
  effectiveValue,
  isOverridden,
  policyName,
  onOverride,
  onReturn
}) {
  return (
    <Form.Item>
      <InputNumber
        value={effectiveValue}
        disabled={!isOverridden}
      />
      {policyName ? (
        isOverridden ? (
          <Tag color="orange">
            ◉ Custom{' '}
            <Button size="small" onClick={onReturn}>
              Return to {policyName}
            </Button>
          </Tag>
        ) : (
          <Tag color="green">
            ◎ Inherited ({policyName}){' '}
            <Button size="small" onClick={onOverride}>
              Override
            </Button>
          </Tag>
        )
      ) : (
        <Tag>Manual</Tag>
      )}
    </Form.Item>
  );
}
```
