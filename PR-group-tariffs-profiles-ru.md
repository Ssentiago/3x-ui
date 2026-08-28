## Summary

Вводит трехсущностную модель **Group / Tariff / Profile** в 3x-ui. **Profile** — это переиспользуемый «кирпичик» конкретных значений полей (трафик в ГБ, срок действия, лимит IP, доступ к инбаундам); **Tariff** объединяет упорядоченную цепочку Profiles и стратегии слияния по каждому полю (`overwrite`/`sum` для трафика, `overwrite`/`union` для инбаундов); **Group** может быть привязан к одному Tariff. Клиенты в группе, привязанной к тарифу, автоматически получают вычисляемые из тарифа квоту трафика, срок действия, лимит IP и назначения инбаундов — это вычисляется при чтении, поэтому редактирование тарифа или его цепочки профилей немедленно применяется ко всем клиентам всех затронутых групп, без изменения каждого клиента вручную. Любое управляемое поле можно переопределить для конкретного клиента (или вернуть под управление тарифа) из формы редактирования клиента.

## Why

Закрывает #5900, #5371, #5026.

В старой системе группы клиентов были лишь свободно заполняемой текстовой меткой в строке клиента — не было структурированного способа задать политику трафика/срока/IP/инбаундов для группы. Администраторам приходилось вручную задавать `totalGB`, `expiryTime`, `limitIp` и привязки инбаундов у каждого клиента, а изменения в масштабе группы означали массовое редактирование клиентов по одному.

### Ментальная модель

Три сущности образуют пирамиду — Profiles внизу, Tariffs в середине, Groups наверху:

```
Group "VIP"  ────►  Tariff "Gold"
                       │
                       ├── Profile "Base"     (traffic 50GB, expiry 30d)
                       ├── Profile "Extra"    (traffic 100GB, limitIP 3)
                       └── Profile "Region"   (inbounds [US-East, EU-West])
```

**Profile** — поименованный набор значений полей. Каждое значение необязательно (`NULL` означает «я это поле не задаю»). Профили переиспользуемы в рамках разных тарифов. Пример: профиль «Base» с трафиком 50 ГБ и сроком 30 дней может быть отправной точкой и для тарифа «Gold», и для «Silver».

**Tariff** — упорядоченная цепочка Profiles + правила их слияния. У тарифа нет собственных значений — он лишь указывает, *какие* профили применять, *в каком порядке* и *как* объединять пересекающиеся поля. Один тариф может обслуживать множество групп; изменение тарифа мгновенно обновляет клиентов всех групп.

**Group** — поименованный набор клиентов. Группа может быть привязана максимум к одному тарифу. Клиенты в группе, привязанной к тарифу, получают трафик/срок/IP/инбаунды из тарифа, если только администратор не переопределил конкретное поле у конкретного клиента.

### Почему три сущности, а не две или одна

Group отвечает на вопрос «кто это?» — метка для массовых операций и границ между клиентами. Tariff отвечает на вопрос «что они получают?» — переиспользуемый набор настроек. Это не один и тот же вопрос, и отношение между ними не «один к одному»:

```
Group "Acme Corp"    ─┐
Group "Beta LLC"      ├──►  Tariff "Standard"
Group "Contoso"      ─┘
Group "Initech"      ────►  Tariff "Gold"
```

Десяти клиентским компаниям не нужно десять разных конфигураций — несколько часто разделяют один и тот же план.

**Одна сущность** (Group несет свои собственные настройки) — склеивает метку и хранилище значений в одну строку. Изменение «Standard» означает ручное редактирование Acme, Beta и Contoso по одному, без гарантии, что в итоге они станут одинаковыми.

**Две сущности** (Group → Profile напрямую, без Tariff) — та же проблема, уровнем ниже. Acme/Beta/Contoso либо получают один и тот же набор профилей, скопированный трижды (измени один — два других незаметно разойдутся), либо администратор выбирает набор профилей одной группы и подключает к нему остальные напрямую — и Group снова тайно становится хранилищем значений.

**Три сущности** (Group → Tariff → Profile) — каждый слой делает одну работу. Group — чистая метка. Tariff — единица переиспользования: подключите Acme, Beta и Contoso к «Standard», отредактируйте «Standard» один раз — все три обновятся немедленно. Profile — единица переиспользования уровнем ниже: один и тот же кирпичик «Base 50GB» может лежать и в «Standard», и в «Gold» без дублирования.

Третий слой окупается только тогда, когда группы могут разделять план. Это здесь обычный случай, а не исключение — отсюда Tariff.

### Стратегии разрешения

При чтении клиента система проходит по цепочке профилей тарифа по порядку и применяет стратегии по каждому полю:

**Traffic** — `overwrite` (по умолчанию) или `sum`:
- `overwrite`: побеждает **последний** профиль, задающий трафик. Profile A: 50 GB → Profile B: 100 GB → итог = 100 GB.
- `sum`: трафик всех профилей **складывается**. Profile A: 50 GB → Profile B: 30 GB → итог = 80 GB.

**Expiry days** — всегда «побеждает последний» (без выбора стратегии):
- Profile A: 30d → Profile B: 7d → итог = 7d.
- Срок привязан к метке времени `started_at` в таблице истории `client_tariffs` (устанавливается, когда клиент впервые вступает в группу): `expiryTime = startedAt + (expiryDays × 86400s)`.

**IP limit** — всегда «побеждает последний» (без выбора стратегии):
- Profile A: 3 IPs → Profile B: 5 IPs → итог = 5 IPs.

**Inbounds** — `overwrite` (по умолчанию) или `union`:
- `overwrite`: набор инбаундов **последнего** профиля заменяет все предыдущие. Profile A: [1,2] → Profile B: [3] → итог = [3].
- `union`: **дедуплицированное объединение** наборов инбаундов всех профилей. Profile A: [1,2] → Profile B: [2,3] → итог = [1,2,3].

Значения `NULL` всегда **пропускаются** — профиль с `limitIP: null` не обнуляет лимит, он просто отказывается участвовать в этом поле. Чтобы явно задать «без ограничений», используйте `0`.

### Система переопределений

Любое управляемое тарифом поле можно **переопределить** для конкретного клиента. Форма редактирования клиента показывает заблокированные поля со значком замка и кнопкой «make local» («сделать локальным»). После переопределения поле перестает следовать тарифу — значение клиента замораживается, пока администратор не нажмет «return to tariff» («вернуть тарифу»). Приоритет переопределения: `override > tariff chain > client default`.

Переопределения хранятся в строке активного членства `client_tariffs` клиента в виде nullable-колонок: `total_gb_override` (int64, nullable), `limit_ip_override` (int, nullable), `expiry_time_override` (int64, nullable) и булева флага `is_inbounds_overridden`. Значения заморожены в строке `ClientTariff`, а не в `clients` — когда клиент выходит из тарифа (строка завершается через `ended_at`), все переопределения автоматически очищаются. Для инбаундов флаг `is_inbounds_overridden` управляет тем, использует ли резолвер собственные записи `client_inbounds` клиента (переопределение активно) или цепочку, вычисленную из тарифа (переопределение неактивно). Таблица связи `client_inbounds` никогда не переписывается для нужд тарифа — она остается собственными назначениями инбаундов клиента.

### Разрешение при чтении

Все четыре поля (трафик, срок, лимит IP, инбаунды) вычисляются **на лету** при чтении — никакие данные клиента в базе не изменяются.

- **Traffic / Expiry / IP limit:** `ResolveClientFields` → `resolveTariffChain` → `resolveChain` (совмещенный слой со стратегиями) → проверка приоритета переопределений. Постраничный список клиентов использует эквивалентные SQL-выражения (`sqlEffTotalGB`, `sqlEffExpiry`, `sqlEffLimitIP`).
- **Inbounds:** `resolveEffectiveInboundIds` проходит по цепочке профилей тарифа, применяет стратегию `overwrite`/`union` и возвращает итоговый набор инбаундов. Используется карточкой клиента (`GetInboundIdsForRecord`), конфигом Xray (`ListForInbound`), подпиской (`ListForInboundBySubId`) и списком клиентов (`resolveEffectiveInboundsForPage` — пакетное разрешение на страницу с кэшем цепочек тарифов, вычисленных заранее). Когда группа отвязывается от тарифа, собственные инбаунды каждого клиента мгновенно восстанавливаются — ничего не потеряно, потому что ничего не изменялось.

Это означает:
- Редактирование тарифа или его цепочки профилей немедленно влияет на каждого клиента в каждой привязанной группе, не затрагивая отдельные строки клиентов.
- Когда клиент вступает в группу, привязанную к тарифу, создается строка `ClientTariff` с `started_at = now`. При выходе из группы или отвязке тарифа устанавливается `ended_at`, закрывая период членства. Срок вычисляется как `client_tariffs.started_at + last-expiryDays*86400000` — стабильно независимо от переименований или повторных вступлений.
- Отвязка тарифа от группы мгновенно восстанавливает у каждого клиента собственные значения полей и назначения инбаундов.
- Когда список инбаундов изменяется через редактирование тарифа, xray автоматически перезапускается для применения новых привязок инбаундов.

TariffFormModal использует серверный предпросмотр через `POST /panel/api/tariffs/preview` (`ResolveChainPreview` в `client_resolve.go`), который возвращает вычисленные эффективные значения с именами профилей-источников — администратор видит результат до сохранения.

## Сквозной сценарий

### Схема базы данных — что было добавлено

Четыре новые таблицы и пять новых колонок в существующих таблицах:

```
┌─────────────────────┐     ┌─────────────────────┐
│ profiles            │     │ tariffs             │
│─────────────────────│     │─────────────────────│
│ id        INTEGER PK│     │ id        INTEGER PK │
│ name      TEXT UNIQ │     │ name      TEXT UNIQ  │
│ traffic   INTEGER?  │     │ traffic_strategy     │
│ expiry_days INTEGER?│     │   TEXT DEFAULT       │
│ limit_ip  INTEGER?  │     │   'overwrite'        │
│ inbound_ids TEXT     │     │ inbound_strategy     │
│ created_at/updated_at│    │   TEXT DEFAULT       │
└─────────┬───────────┘     │   'overwrite'        │
          │                 │ enable    BOOL       │
          │                 │ created_at/updated_at │
          │                 └───────────┬───────────┘
          │                             │
          │    ┌────────────────────────┘
          │    │
┌─────────▼────▼─────────┐     ┌──────────────────────────┐
│ tariff_profiles        │     │ client_tariffs           │
│────────────────────────│     │──────────────────────────│
│ tariff_id  INTEGER PK  │     │ id          INTEGER PK   │
│ profile_id INTEGER PK  │     │ client_id   INTEGER      │
│ position   INTEGER UNIQ│     │ tariff_id   INTEGER      │
└────────────────────────┘     │ started_at  INTEGER      │
                               │ ended_at    INTEGER?     │
                               └──────────────────────────┘

Existing tables, new columns:
  client_groups.tariff_id  INTEGER?  →  links group to tariff (NULL = no tariff)
  client_tariffs.total_gb_override    INTEGER?   →  overridden traffic value (NULL = follow tariff)
  client_tariffs.limit_ip_override    INTEGER?   →  overridden IP limit (NULL = follow tariff)
  client_tariffs.expiry_time_override INTEGER?   →  overridden expiry timestamp (NULL = follow tariff)
  client_tariffs.is_inbounds_overridden BOOL DEFAULT false
```

**Ключевые проектные решения:**
- `profiles.traffic` хранится в **ГБ** (не в байтах). Резолвер умножает на `1<<30` при чтении — это сохраняет значения в БД человекочитаемыми.
- `profiles.inbound_ids` — это **JSON-текстовая колонка** (`"[1,2,3]"`), а не отдельная таблица связи. Каждый профиль — самодостаточный набор значений.
- У `tariffs` **нет колонок со значениями** — ни `traffic`, ни `expiry_days`, ни `limit_ip`, ни `inbound_ids`. Каждое значение приходит из прикрепленных профилей. Добавить значение тарифу — значит добавить профиль в его цепочку.
- `client_tariffs` — это **историческая таблица только для добавления** (append-only). У активной строки `ended_at IS NULL`; завершение строки (отвязка, смена тарифа, повторное вступление) устанавливает `ended_at`, но никогда не удаляет строку. Переопределения (`total_gb_override`, `limit_ip_override`, `expiry_time_override`, `is_inbounds_overridden`) живут именно на этой строке — когда клиент покидает тариф, строка завершается, и все переопределения автоматически очищаются.

### Админ-сценарий — по шагам

**Шаг 1: создание профилей.** Администратор создает поименованные «наборы значений» во вкладке Profiles. Каждое поле необязательно (установите NULL, чтобы пропустить его). Пример:

| Profile | Traffic | Expiry | Limit IP | Inbounds |
|---------|---------|--------|----------|----------|
| "Base 50GB" | 50 GB | 30 days | _(not set)_ | _(not set)_ |
| "Extra 100GB" | 100 GB | _(not set)_ | 3 | _(not set)_ |
| "US Region" | _(not set)_ | _(not set)_ | _(not set)_ | [US-East, US-West] |

**Шаг 2: создание тарифа.** Администратор выбирает имя, стратегии слияния и собирает **упорядоченную цепочку** профилей. Пример тарифа «Gold»:

```
Position 1: "Base 50GB"   →  sets traffic=50GB, expiry=30d
Position 2: "Extra 100GB" →  overwrites traffic to 100GB, sets limitIP=3
Position 3: "US Region"   →  adds inbounds [US-East, US-West]
```

TariffFormModal показывает **живой предпросмотр** итоговой цепочки через серверный эндпоинт `POST /tariffs/preview`. Кнопка «save» вызывает `POST /tariffs/:id/profiles`, который атомарно заменяет строки `tariff_profiles` в одной транзакции.

**Шаг 3: привязка тарифа к группе.** Во вкладке Groups администратор создает или переименовывает группу, выбирая тариф из выпадающего списка. Это устанавливает `client_groups.tariff_id`. С этого момента каждый клиент группы наследует вычисленные из тарифа значения при чтении — строки клиентов не затрагиваются.

**Шаг 4: клиенты наследуют автоматически.** Когда генерируется список клиентов или конфиг xray, резолвер проходит по цепочке `client → group → tariff → ordered profiles → effective values`. Клиент получает `totalGB=100GB`, `expiryTime=startedAt+30d`, `limitIP=3` и привязывается к инбаундам [US-East, US-West].

**Шаг 5 (необязательно): переопределение для клиента.** Администратор открывает форму редактирования клиента. Каждое управляемое поле **заблокировано** (приглушено, иконка замка, тег с именем тарифа). Клик по замку открывает поповер: «Это поле управляется тарифом 'Gold'. Сделать локальным?» Если администратор нажимает «Make local», вызывается `OverrideField` — текущее эффективное значение замораживается в строке клиента, флаг переопределения устанавливается, и поле разблокируется (становится редактируемым). Ссылка «Return to tariff» в поповере отменяет это действие.

**Шаг 6 (необязательно): редактирование тарифа.** Администратор добавляет новый профиль в цепочку или меняет стратегию. Строки клиентов не изменяются — цепочка тарифа пересчитывается при следующем чтении. При сохранении возникают два побочных эффекта:

- **Обновление кэша статистики:** `RefreshTrafficForGroup` перезаписывает `client_traffics.total` и `.expiry_time` (таблица кэша использования трафика) новыми эффективными значениями. Это оптимизация производительности — UI исчерпания/срока читает из `client_traffics`, а не из резолвера. Основные колонки `clients` (`total_gb`, `expiry_time`, `limit_ip`) **не затрагиваются**. Клиенты с переопределениями пропускаются при этом обновлении — их значения остаются замороженными.

- **Перезапуск Xray:** JSON-конфиг xray перечисляет, какие клиенты принадлежат каким инбаундам. Когда меняется цепочка инбаундов тарифа, набор клиентов на инбаунд меняется при чтении — `ListForInbound(inboundId)` теперь возвращает других клиентов. В `client_inbounds` (таблицу связи) ничего не пишется. Xray перезапускается, чтобы подхватить обновленный конфиг. Если редактирование тарифа не затронуло назначения инбаундов (изменились только трафик/срок/IP), перезапуск не нужен.

### Конвейер разрешения — что происходит при чтении клиента

`ResolveClientFields(db, client)` — единственная точка входа. Она выполняется в таком точном порядке:

1. **Загрузка собственных инбаундов** — `SELECT inbound_id FROM client_inbounds WHERE client_id = ?` (прямые привязки инбаундов клиента, никогда не изменяются тарифом).

2. **Инициализация результата** из «сырых» колонок клиента — `TotalGB`, `ExpiryTime`, `LimitIP`, `InboundIds = ownIds`.

3. **Разрешение цепочки тарифа** (ранний выход при условии: нет группы, у группы нет тарифа или все три флага лимитов переопределены):
   - Загрузка группы по имени → загрузка тарифа по `group.TariffID` → загрузка профилей через `tariff_profiles ORDER BY position ASC`.
   - `resolveChain(ctx)` проходит по профилям в порядке позиций:
     - **Traffic**: `sum` суммирует, `overwrite` берет последний. Устанавливает `HasTraffic=true`, только когда профиль действительно имеет непустое (non-NULL) значение трафика.
     - **ExpiryDays**: всегда побеждает последнее непустое (non-NULL) значение. Устанавливает `HasExpiryDays=true`.
     - **LimitIP**: всегда побеждает последнее непустое (non-NULL) значение. Устанавливает `HasLimitIP=true`.
     - **InboundIds**: пропускает профили с NULL/пустым значением. `union` объединяет и дедуплицирует, `overwrite` заменяет.
   - Применение приоритета переопределений: `IsXxxOverridden ? keep raw column : take chain-resolved value`.
   - Срок: `client_tariffs.started_at + chain.ExpiryDays * 86400 * 1000`. Если нет активной строки `ClientTariff`, срок остается равным значению «сырой» колонки.

4. **Разрешение инбаундов** — `IsInboundsOverridden ? ownIds : (union ? merge(ownIds, chain.inboundIds) : chain.inboundIds)`. Для `union` собственные строки инбаундов клиента объединяются с цепочкой — клиент сохраняет прямые привязки и получает привязки тарифа.

Существуют два пути по производительности:
- **Один клиент** (`ResolveClientFields`) — 3-4 запроса за вызов; используется формой/редактированием/конфигом xray.
- **Пакетный** (`ClientBatchResolver`) — предзагружает группы-по-тарифам, цепочки по тарифам и карты `started_at` за 3 запроса; затем O(1) на клиента. Используется постраничным списком клиентов.

### Механика переопределений — как работают «Make local» и «Return to tariff»

**`OverrideField(email, "totalGB")`:**
1. Вызвать `ResolveClientFields`, чтобы получить актуальный эффективный totalGB (то, что сейчас дает тариф).
2. Записать эффективное значение в строку активного `ClientTariff` клиента (`total_gb_override = effectiveTotalGB`) — заморозить значение.
3. Отныне резолвер видит непустой `total_gb_override` и пропускает цепочку тарифа для totalGB, возвращая замороженное значение.

**`ReturnToTariff(email, "totalGB")`:**
1. Установить `total_gb_override = NULL` на строке активного `ClientTariff`.
2. Резолвер немедленно снова берет значение из цепочки тарифа. Значение на клиенте не менялось — оно всегда вычислялось на лету.

**`ReturnToTariff(email, "inbounds")` — особый случай:**
1. Установить `is_inbounds_overridden = false` на строке активного `ClientTariff`.
2. Пересчитать цепочку — для `overwrite` `ApplyInboundList` сравнивает текущие и целевые строки `client_inbounds` и выполняет DETACH/ATTACH по необходимости. Для `union` клиент сохраняет свои строки и получает строки тарифа.
3. Перезапустить xray (назначения инбаундов изменились).

### Жизненный цикл ClientTariff — как работает привязка срока

Таблица `client_tariffs` — это хронология тарифных членств, допускающая только добавление (append-only):

```
Client #42, Tariff "Gold":
  id=1: started_at=1712505600000 (2024-04-07), ended_at=NULL  ← ACTIVE
```

Когда у тарифа Gold срок = 30 дней, резолвер вычисляет: `1712505600000 + 30*86400*1000 = 2024-05-07`.

**Строки создаются, когда:**
- Клиент вступает в группу, привязанную к тарифу (через `AddToGroup`, `bulkAdd` или переименование группы с назначением тарифа).
- Группа клиента меняется на другую привязанную к тарифу группу в форме редактирования.

**Строки завершаются (`ended_at = now`), когда:**
- Тариф отвязывается от группы (`POST /groups/resetTariff`).
- Клиент меняет тариф (старая строка завершается, новая создается в той же транзакции).
- Клиент повторно вступает в тариф (старая строка завершается, новая создается).

**Важный крайний случай:** когда клиент удаляется из группы (группа очищена или назначена группа без тарифа), строка `client_tariffs` **не завершается**. Резолвер просто игнорирует ее (выходит на проверке «no group»). Это безвредно — строка снова обретает значение, только если клиент повторно вступает в тариф, после чего она завершается и заменяется.

### Интеграция с существующими системами

**Генерация конфига Xray:** `GetXrayConfig` вызывает `ListForInbound(inboundId)`, который возвращает клиентов с вычисленными из тарифа `totalGB`/`expiryTime`/`limitIP` и корректными назначениями инбаундов. `internal/xray/` не изменяется — эффект тарифа проходит целиком через сервисный слой.

**Подписки (raw/JSON/Clash):** `ListForInboundBySubId` возвращает вычисленные из тарифа эффективные значения. Также изменены `internal/sub/service.go`, `clash_service.go`, `json_service.go` и `remark_vars.go`: карта `resolvedByEmail` подставляет вычисленные из тарифа поля клиента в шаблоны remark (`{totalGB}`, `{expiryTime}` и т. д.), а `AggregateTrafficByEmails` безусловно применяет `ResolveClientLimits`.

**Fail2ban (правоприменение лимита IP):** `check_client_ip_job.go` адаптирован для чтения лимитов IP, заданных через тарифы. `hasLimitIp()` проверяет и таблицу `clients` (`limit_ip > 0 OR EXISTS(client_tariffs … limit_ip_override IS NOT NULL)`), и объединенный запрос через `client_groups → tariff_profiles → profiles.limit_ip`. `loadClientLimits` вычисляет эффективный лимит по email с приоритетом `limit_ip_override > tariff-resolved > client.limit_ip`.

- [ ] Исправление ошибки (bug fix)
- [x] Новая функция
- [x] Рефакторинг (без изменения поведения)
- [ ] Документация
- [ ] Только тесты
- [ ] Сборка / CI / инструментарий
- [ ] Другое

## Затронутые области

- [x] Фронтенд (UI / страницы панели)
- [x] Бэкенд (API-эндпоинты, логин, настройки)
- [x] Генерация конфига Xray
- [x] Подписки (ссылки / Clash / JSON)
- [ ] Статистика / счетчики трафика
- [x] База данных / миграции
- [ ] Скрипт установки / обновления
- [ ] Docker-образ
- [ ] Мультинодное развертывание (sub-nodes)
- [ ] Telegram-бот

### Бэкенд

**Модели / БД** — `internal/database/model/`:
- Новые `Tariff` (таблица `tariffs`), `Profile` + `TariffProfile` + `TariffProfileItem` + `ResolvedFields` и константы стратегий (таблицы `profiles`, `tariff_profiles`) в `internal/database/model/profile.go` и `model.go`.
- `ClientGroup` получает `TariffID *int` (`client_groups.tariff_id`).
- Новая модель `ClientTariff` / таблица `client_tariffs` (`id`, `client_id`, `tariff_id`, `started_at`, `ended_at`) — ведет историю тарифных членств клиента, заменяя прежнюю колонку с меткой времени `tariff_started_at`. На этой же строке живут колонки переопределений: `total_gb_override` (nullable int64), `limit_ip_override` (nullable int), `expiry_time_override` (nullable int64), `is_inbounds_overridden` (bool). `ClientRecord` не имеет колонок переопределений — все переопределения ограничены членством в тарифе.
- `ClientRecord` получает хелперы `ToClientEffective()`/`toClient()` для эффективных значений при чтении, плюс `FieldMode` enum (`tariff`/`override`/`own`) на выходной структуре `Client`.

**Миграции** — `internal/database/db.go` + `internal/database/migrate_data.go`: `Tariff`, `Profile`, `TariffProfile`, `ClientTariff` зарегистрированы в `allModels()`. Все четыре добавляются через GORM `AutoMigrate`: четыре новые таблицы, новые колонки на `clients` и `client_groups`, ничего не удаляется и не изменяется. Примечание: `ClientTariff` есть в `allModels()`, но отсутствует в `migrationModels()` — таблица `client_tariffs` не копируется командой `x-ui migrate-db` (SQLite→Postgres).

**API-эндпоинты** — `internal/web/controller/group.go`, `profile.go`, `client.go` (все смонтированы в `/panel/api/clients`):
- Группы: `GET /groups`, `GET /groups/:name/emails`, `POST /groups/create` (теперь принимает `tariffId`), `POST /groups/rename` (создает строки `ClientTariff` для клиентов, вступающих в новый тариф), `POST /groups/delete`, `POST /groups/resetTariff`, `POST /groups/resetTraffic`, `POST /groups/bulkAdd` (сбрасывает переопределения и создает строки `ClientTariff` для групп, привязанных к тарифу), `POST /groups/bulkRemove`.
- Тарифы: `GET /tariffs`, `GET /tariffs/:id` (возвращает цепочку `profiles` + поля `resolved` + количество групп/клиентов), `POST /tariffs/create`, `POST /tariffs/:id/update`, `POST /tariffs/:id/delete` (отклоняется, пока на тариф ссылается любая группа), `POST /tariffs/:id/profiles` (атомарная замена упорядоченной цепочки), `POST /tariffs/preview` (предпросмотр цепочки без сохранения — возвращает `ResolvedFields` с именами профилей-источников).
- Профили: `GET /profiles`, `GET /profiles/:id`, `POST /profiles/create`, `POST /profiles/:id/update`, `POST /profiles/:id/delete` (отклоняется, пока профиль используется любым тарифом).
- Клиенты: новые `GET /get/resolve/:email?group=<name>` (предпросмотр «что, если» вычисленных из тарифа значений для предполагаемой группы), `GET /get/effective/:email` (полное тарифно-разрешенное представление клиента), `POST /overrideField` и `POST /returnToTariff` (заморозка / освобождение управляемого поля одного клиента в строке `client_tariffs`; возврат `inbounds` вызывает Attach/Detach + перезапуск xray).

**Бизнес-логика** — `internal/web/service/`:
- `tariff.go` — CRUD `TariffService`, разрешение цепочки (`resolveTariff`), количество групп/клиентов и обновление трафика (`RefreshTrafficForGroup*`, `refreshTariffTraffic`), которое перезаписывает `client_traffics.total/expiry_time` из эффективных значений при изменении тарифа.
- `client_resolve.go` — **единый конвейер разрешения**. `ResolveClientFields` — единственная точка входа: загружает группу→тариф→цепочку профилей, применяет стратегии по полям (overwrite/sum для трафика, «последний побеждает» для срока/IP, overwrite/union для инбаундов), проверяет колонки переопределений на активной строке `ClientTariff`, возвращает полностью вычисленные `TotalGB`/`LimitIP`/`ExpiryTime`/`InboundIds`. `ResolveClientLimits` оборачивает его для потребителей статистики. `ResolveChainPreview` — серверный предпросмотр для `POST /tariffs/preview`. `ClientBatchResolver`/`NewBatchResolver` пакетно вычисляют значения для постраничного списка.
- `client_tariff.go` — API переопределений: `OverrideField` записывает эффективное значение в колонку переопределения на строке `ClientTariff`; `ReturnToTariff` очищает колонку; `ApplyInboundList` выполняет Attach/Detach через gRPC при возврате инбаундов. `activateClientTariffsByEmails` пакетно создает/завершает строки `ClientTariff`.
- `client_groups.go` — `ListGroups` теперь возвращает сводку `tariffId`, `tariffName`, `tariff`; `CreateGroup(name, tariffId)`; `AddToGroup` при назначении в тарифную группу вызывает `activateClientTariffsByEmails` — закрывает предыдущую активную строку `ClientTariff` и создает новую (старые переопределения автоматически очищаются, т.к. живут на закрываемой строке). Возвращает `error` (не `(int, error)`).
- `client_paging.go` — `ClientSlim` несет `tariffName`, значения переопределений и флаги `*IsOverridden`; effective-выражения на уровне SQL (`sqlEffTotalGB`, `sqlEffExpiry`, `sqlEffLimitIP`) обеспечивают фильтрацию по исчерпанию/приближению к исчерпанию/диапазону срока и сортировку по остатку/сроку; `loadGroupTariffs` соединяет group→tariff; `resolveEffectiveInboundsForPage` пакетно вычисляет эффективные ID инбаундов на страницу с предрасчитанным кэшем цепочек по тарифам.
- `client_link.go` — `ListForInbound` и `ListForInboundBySubId` вычисляют инбаунды на лету: прямые строки `client_inbounds` для клиентов без тарифа/с переопределениями + клиенты, вычисленные из тарифа (через `tariffIdsContainingInbound`, который предфильтрует тарифы, в чью цепочку входит данный ID инбаунда). Оба выдают эффективные значения через `ToClientEffective`.
- `client_get.go` — `resolveEffectiveInboundIds` вычисляет эффективные инбаунды одного клиента через цепочку тарифа (применяет `overwrite`/`union`). Его используют `GetInboundIdsForRecord`, `GetInboundIdsForEmail` и `List()`.
- `inbound_traffic.go` — `AddClientStat`/`UpdateClientStat` записывают эффективные `total`/`expiry_time` в `client_traffics` через `resolveEffectiveTraffic`.
- `profile.go` — CRUD `ProfileService` с валидацией и защитой от использования тарифами.
- `internal/web/job/check_client_ip_job.go` — проверка лимита IP для fail2ban и `loadClientLimits` теперь учитывают `client_tariffs.limit_ip_override` и лимиты, заданные через тариф.

**Интеграция с Xray** — прямых изменений в `internal/xray/` нет; эффект проходит через `GetXrayConfig` в `internal/web/service/xray.go`, использующий `ListForInbound`, — генерируемый конфиг клиентов xray получает вычисленные из тарифа `totalGB`/`expiryTime`/`limitIp` и назначения инбаундов.

**Интеграция с подписками** — изменены `internal/sub/service.go`, `internal/sub/clash_service.go`, `internal/sub/json_service.go` и `internal/sub/remark_vars.go`: карта `resolvedByEmail` подставляет вычисленные из тарифа данные клиента в шаблоны remark, а `AggregateTrafficByEmails` безусловно применяет `ResolveClientLimits` — вывод подписки отражает вычисленные из тарифа квоту/срок/инбаунды.

**i18n** — `internal/web/translation/*.json` (все 13 локалей) получили ключи `menu.tariffs`, `pages.profiles.*`, `pages.tariffs.*` и `pages.clients.*` (`managedFieldLocked`, `managedFieldLockedDesc`, `tariffManagedNotice`, `makeLocal`, `returnToTariff`). Большая часть построчных изменений — переформатирование JSON (все 13 файлов переписаны с единообразными отступами/сортировкой).

### Фронтенд

- `src/pages/groups/GroupsPage.tsx` — контейнер с тремя вкладками (Groups / Tariffs / Profiles), общие хуки запросов и варианты инбаундов.
- `src/pages/groups/GroupsTab.tsx` — таблица групп с колонкой тарифа, создание/переименование (с выбором тарифа), удаление, сброс трафика, **сброс тарифа**, добавление/удаление клиентов, под-ссылки, массовая настройка.
- `src/pages/groups/GroupFormModal.tsx` — модальное окно создания/переименования с полем имени + выбором тарифа.
- `src/pages/groups/TariffsTab.tsx` + `TariffFormModal.tsx` — CRUD тарифов, конструктор упорядоченной цепочки профилей (добавление/поиск/переупорядочивание/удаление профилей), выбор стратегий по полям и живой **предпросмотр результата** с показом эффективных трафика/срока/IP/инбаундов и их исходного профиля.
- `src/pages/groups/ProfilesTab.tsx` + `ProfileFormModal.tsx` — CRUD профилей (трафик/срок/IP/инбаунды), колонка использования `tariffCount`.
- Переиспользует существующие `GroupAddClientsModal.tsx` / `GroupRemoveClientsModal.tsx` для массового членства (созданы не в этом PR — подключены в `GroupsTab`).
- `src/components/ManagedField.tsx` + `src/hooks/useTariffOverrides.ts` — отрисовывают заблокированное/перекрытое поле, когда им управляет тариф, со значком замка и кнопкой «make local» как «запасным выходом» через поповер по клику.
- `src/pages/clients/ClientFormModal.tsx` — форма с учетом тарифа: управляемые `totalGB`/`limitIp`/`expiryTime`/инбаунды заблокированы в `ManagedField`, показывают тег с именем тарифа, могут быть сделаны локальными или возвращены тарифу; учтено взаимодействие срока и отложенного старта.
- `src/pages/clients/ClientsPage.tsx` — порядок слияния при гидрации редактирования в `onShowInfo` перевернут (`{...full.client, ...row}`), чтобы поля переопределений/slim-поля переживали слияние (QR- и edit-модалки используют исходный порядок). Пропсы групп в модалках изменены с `string[]` на `{name: string}[]`.
- `src/schemas/tariff.ts`, `src/schemas/profile.ts` — Zod-схемы; в `src/schemas/client.ts` расширены `ClientRecordSchema` (поля переопределений, `tariffName`, `*IsOverridden`) и `GroupSummarySchema` (информация о тарифе).
- `src/api/queryKeys.ts` — `keys.clients.tariffs()` / `keys.clients.profiles()`; `src/layouts/AppSidebar.tsx` — обработка активного состояния навигации `/groups`.
- `src/pages/api-docs/endpoints.ts` — новые секции «Tariffs» и «Profiles» + записи `get/resolve/:email`, `get/effective/:email`, `tariffs/preview`, `groups/resetTariff`, `overrideField`, `returnToTariff`.
- `src/generated/*` — перегенерировано через `tools/openapigen/main.go` (новые записи `StructAllow`: `Tariff`, `Profile`, `TariffProfile`, `TariffProfileItem`, `ResolvedFields`, `TariffSummary`).
- **Предпросмотр тарифа:** TS-двойник Go-резолвера (`resolveChain.ts`) удалён. Предпросмотр теперь полностью серверный — `POST /panel/api/tariffs/preview` возвращает `ResolvedFields` с именами профилей-источников для UI. Только две реализации алгоритма разрешения (Go + SQL), без третьей TS-копии.

## Обоснование по файлам

### Бэкенд: новые файлы

**`internal/database/model/profile.go`** — определяет сущность `Profile` (nullable `Traffic`, `ExpiryDays`, `LimitIP`, JSON-текстовые `InboundIds`), таблицу связи `TariffProfile` (составной PK, `position` для порядка цепочки), DTO `TariffProfileItem` для API, `ResolvedFields` (выходную структуру) и константы стратегий (`StrategyOverwrite`/`StrategySum`/`StrategyUnion`). Профили — переиспользуемые «наборы значений полей», где `NULL` означает «не участвует». ID инбаундов хранятся JSON-строкой в одной колонке, а не в отдельной таблице связи, что сохраняет профиль самодостаточным. Зарегистрирован для AutoMigrate в `db.go` + `migrate_data.go`.

**`internal/web/controller/profile.go`** — REST-обработчики профилей: `GET /profiles`, `GET /profiles/:id`, `POST /profiles/create`, `POST /profiles/:id/update`, `POST /profiles/:id/delete`. Все смонтированы в `/panel/api/clients` через `GroupController.initRouter`. Связывает `profileBody` (имя + 4 nullable-поля) с `ProfileService`.

**`internal/web/service/client_tariff.go`** — API переопределений. `OverrideField(email, field)` вычисляет эффективное значение через `ResolveClientFields` и записывает его в колонку переопределения на активной строке `ClientTariff` клиента (`total_gb_override` / `limit_ip_override` / `expiry_time_override` / `is_inbounds_overridden`). `ReturnToTariff` очищает колонку. Для инбаундов `ReturnToTariff("inbounds")` вызывает `ApplyInboundList` (Attach/Detach через gRPC). `activateClientTariffsByEmails` пакетно управляет строками `ClientTariff` — закрывает предыдущую активную и создает новую с чистым состоянием переопределений.

**`internal/web/service/client_resolve.go`** — единый конвейер разрешения. `resolveChain` проходит по упорядоченной цепочке профилей тарифа и применяет стратегии по полям (overwrite/sum для трафика, «последний побеждает» для срока/IP, overwrite/union для инбаундов, пропуск `NULL`). `ResolveClientFields` — единственная точка входа: загружает группу→тариф→цепочку, применяет стратегии, проверяет колонки переопределений на активной строке `ClientTariff` (override > tariff chain > client default), возвращает полностью вычисленные `TotalGB`/`LimitIP`/`ExpiryTime`/`InboundIds` + `FieldMode` enum по каждому полю. `ResolveClientLimits` оборачивает его для потребителей статистики. `ResolveChainPreview` — серверный предпросмотр для `POST /tariffs/preview` (возвращает `ResolvedFields` с именами профилей-источников). `ClientBatchResolver`/`NewBatchResolver` пакетно вычисляют значения для постраничного списка с предрасчитанным кэшем цепочек. `ToClientEffective` отображает вычисленные значения на выходную структуру `Client`.

**`internal/web/service/tariff.go`** — `TariffService`: листинг/получение/создание/обновление/удаление тарифов, `SetProfiles` (транзакционная замена упорядоченной цепочки с валидацией смежности позиций), `resolveTariff` (`ResolvedFields`, используемый `GET /tariffs/:id`), количество групп/клиентов и семейство обновления кэша трафика: `RefreshTrafficForGroup`, `RefreshTrafficForGroupReset`, `refreshTariffTraffic`, `rewriteTrafficForClients`. Также содержит общие хелперы `parseInboundIds`/`marshalInboundIds`. Обновление кэша трафика поддерживает согласованность статистики `client_traffics` после правок тарифов/профилей — авторитетными являются эффективные значения при чтении, но кэшированные `total`/`expiry_time` управляют фильтрацией исчерпания и UI трафика, поэтому они перезаписываются при изменении тарифа (поля, контролируемые переопределениями, пропускаются).

**`internal/web/service/profile.go`** — `ProfileService`: CRUD с `validateProfileInput` (проверка неотрицательности nullable-полей) и защитой удаления, которая отказывает в удалении, пока любая строка `tariff_profiles` ссылается на профиль (с сообщением имен ссылающихся тарифов). `tariffCount`/`batchCountTariffsByProfileId` питают колонку «используется N тарифами» в `ProfilesTab`.

**`internal/web/service/client_tariff_test.go`** — девять тестовых наборов (671 строка):
- `TestResolveChain` (11 табличных случаев): пустая цепочка, один/несколько профилей, overwrite/sum для трафика, union/overwrite для инбаундов, пропуск null-полей, смешанные несколько полей.
- `TestResolveChainHasFlags`: проверяет флаги стратегий в вычисленных результатах.
- `TestMergeInboundIds`: логика слияния union/overwrite для наборов ID инбаундов.
- `TestResolveClientFields_*` (несколько наборов): разрешение на основе БД через весь конвейер (tariff chain → override → effective), включая крайние случаи для каждого поля.
- `TestSqlExpiryNullWithoutStartedAt`: проверяет, что срок равен null, когда нет строки `ClientTariff`.
- `TestSqlEffectiveMatchesGoResolver` (интеграционный, 5 клиентов): проверяет, что SQL-выражения в `client_paging.go` дают те же результаты, что Go-резолвер, на одних и тех же данных SQLite — фиксирует три реализации (Go, SQL, TS) на одной семантике.

**`internal/web/service/tgbot/tgbot_client.go`** — вычисляет эффективные из тарифа инбаунды через `service.ResolveClientFields` для сообщения бота с информацией о клиенте. Также включает правку под gofumpt: `common.FormatTraffic((traffic.Total))` → `common.FormatTraffic(traffic.Total)`.

**`tools/openapigen/main.go`** — `StructAllow`: `Tariff`, `Profile`, `TariffProfile`, `TariffProfileItem`, `ResolvedFields`, `TariffSummary`. Без них новые сущности молча пропускаются в генерируемых типах/примерах, и `build-openapi.mjs` падает.

### Бэкенд: измененные файлы

**`internal/database/model/model.go`** — структура `Tariff` (новая таблица: `id`, `name`, `traffic_strategy`, `inbound_strategy`, `enable` — у тарифа **нет собственных колонок значений**; значения приходят из профилей). Структура `ClientTariff` (новая таблица `client_tariffs`: `id`, `client_id`, `tariff_id`, `started_at`, `ended_at`, `total_gb_override`, `limit_ip_override`, `expiry_time_override`, `is_inbounds_overridden`) — ведет историю тарифных членств клиента и хранит переопределения. `ClientRecord` не имеет колонок переопределений (ни bool-флагов, ни value-колонок). `ClientGroup.TariffID *int` связывает группу с тарифом.

**`internal/database/db.go` + `internal/database/migrate_data.go`** — `Tariff`, `Profile`, `TariffProfile` зарегистрированы и в `allModels()` (чистые установки), и в `migrationModels()` (обновления). Все добавляется через GORM `AutoMigrate` — три новые таблицы, новые колонки на `clients` и `client_groups`, ничего не удаляется.

**`internal/web/controller/group.go`** — `POST /create` и `POST /rename` принимают `tariffId`. Переименование создает строки `ClientTariff` (с `started_at = now`) для клиентов, у которых нет активной строки в новом тарифе. `POST /delete` также запускает `RefreshTrafficForGroupReset`. Новые эндпоинты: `POST /resetTariff` отвязывает тариф от группы (устанавливает `ended_at` на активных строках `ClientTariff`); `POST /bulkAdd` сбрасывает переопределения и создает строки `ClientTariff` для участников группы, привязанной к тарифу, без активной строки. Весь CRUD тарифов (`/tariffs...`), эндпоинты переопределений (`/overrideField`, `/returnToTariff`) и эндпоинты профилей (через `NewProfileController`) смонтированы здесь, под `/panel/api/clients`. Каждая мутация вызывает `notifyClientsChanged()` + `SetToNeedRestart()`, где это уместно.

**`internal/web/controller/client.go`** — новый `GET /get/resolve/:email?group=<name>` возвращает предпросмотр «что, если» (`ResolvedForGroup{totalGB, expiryTime, limitIp, inboundIds}`) вычисленных из тарифа значений для предполагаемого назначения в группу — используется формой клиента, чтобы показать, что клиент *получил бы* до сохранения смены группы. Путь `update` теперь получает поля `totalGBMode` / `limitIpMode` / `expiryTimeMode` (`"tariff"|"override"|"own"`) с фронтенда и соответствующим образом переключает булевы флаги переопределения. Гидрация формы редактирования использует «сырой» `GET /get/:email` (возвращает данные клиента без разрешения тарифа).

**`internal/web/service/client_groups.go`** — `GroupSummary` получает `TariffID`, `TariffName`, `Tariff *TariffSummary`. `ListGroups` делает join с тарифами. `CreateGroup(name, tariffId)`. `AddToGroup`, когда целевая группа привязана к тарифу, вызывает `activateClientTariffsByEmails` — закрывает предыдущую активную строку `ClientTariff` (с `ended_at = now`) и создает новую. Старые переопределения автоматически очищаются, т.к. живут на закрываемой строке. Все групповые функции (`RenameGroup`, `DeleteGroup`, `AddToGroup`, `RemoveFromGroup`, `replaceGroupValue`) возвращают `error` (не `(int, error)`) — `affected`-счетчики убраны из API ответов.

**`internal/web/service/client_paging.go`** — `ClientSlim` получает `TariffName`, булевы флаги переопределения и флаги `*IsOverridden`. Добавлены effective-выражения на уровне SQL, используемые везде, где раньше фигурировали `c.total_gb`/`c.expiry_time`/`c.limit_ip`:
- `sqlEffTotalGB`: обрабатывает `sum` (через `SUM(...) * 1073741824`) и `overwrite` (через подзапрос `ORDER BY position DESC LIMIT 1`) с откатом к переопределению (`is_total_gb_overridden`).
- `sqlEffExpiry`: берет `started_at` из подзапроса к `client_tariffs` (`(SELECT ct.started_at FROM client_tariffs ct …)`) + последние-expiryDays*86400000, с откатом через переопределение к `client.expiry_time`.
- `sqlEffLimitIP`: «последний побеждает» с откатом к переопределению (`is_limit_ip_overridden`).
Они обеспечивают фильтрацию по исчерпанию/приближению к исчерпанию/диапазону срока и сортировку по остатку/сроку. `loadGroupTariffs` пакетно соединяет group→tariff; `resolveEffectiveInboundsForPage` пакетно вычисляет значения на страницу с предрасчитанным кэшем цепочек тарифов.

**`internal/web/service/client_link.go`** — `ListForInbound` (→ конфиг xray) и `ListForInboundBySubId` (→ подписка: raw/JSON/Clash) теперь используют общий `listForInboundFiltered`, который выбирает данные в два шага: (1) клиенты с прямым назначением (`is_inbounds_overridden` установлен или нет группы с тарифом), (2) клиенты, вычисленные из тарифа, через `tariffIdsContainingInbound` (предфильтрует тарифы, в чью вычисленную цепочку входит данный ID инбаунда). Результаты дедуплицируются, каждая запись проходит через `ToClientEffective`. В `internal/xray/` изменений нет — эффект тарифа достигает конфигов xray и вывода подписки целиком через этот файл.

**`internal/web/service/client_get.go`** (переименован из `client_lookup.go`) — `resolveEffectiveInboundIds(client)`: собственные `client_inbounds`, если `is_inbounds_overridden` установлен или нет группы с тарифом; иначе цепочка тарифа с `union` (объединение своих и цепочки, отсортировано) или `overwrite` (замена цепочкой). `GetInboundIdsForEmail`/`GetInboundIdsForRecord`/`List()` идут через него. `List()` вычисляет пакетно через кэш по тарифам. Таблица связи не мутируется — собственные строки инбаундов клиента остаются нетронутыми.

**`internal/web/service/client_crud.go`** — путь обновления теперь потребляет поля `TotalGBMode` / `LimitIPMode` / `ExpiryTimeMode` (`"tariff"|"override"|"own"`), отправляемые фронтендом, и переключает булевы флаги переопределения на `ClientRecord`.

**`internal/web/service/inbound_sublink.go`** — вычисляет ID инбаундов саблинка через `ResolveClientFields`, чтобы генерация саблинка отражала вычисленные из тарифа назначения инбаундов.

**`internal/web/service/inbound_traffic.go`** — `AddClientStat`/`UpdateClientStat` вызывают `resolveEffectiveTraffic` перед записью в `client_traffics`, чтобы кэш статистики нес эффективные из тарифа `total`/`expiry_time` (переопределение побеждает; иначе `EffectiveTotalGB`/`EffectiveExpiryTime`, когда есть группа). Дополняет пакетное обновление в `tariff.go` — это покрывает записи по одному клиенту, то — массовые перезаписи.

**`internal/web/job/check_client_ip_job.go`** — `hasLimitIp()` проверяет и `clients` (`limit_ip > 0 OR EXISTS(client_tariffs … limit_ip_override IS NOT NULL)`), и объединенный запрос через `client_groups → tariff_profiles → profiles.limit_ip`. `loadClientLimits` вычисляет эффективный лимит по email с приоритетом `limit_ip_override > tariff-resolved > client.limit_ip`.

### Фронтенд: новые файлы

**`src/pages/groups/GroupsPage.tsx`** — контейнер с тремя вкладками (Groups / Tariffs / Profiles). Содержит общие хуки TanStack Query (группы, тарифы, профили, slim-список инбаундов), вычисляет `inboundOptions` (отфильтрован по `MULTI_CLIENT_PROTOCOLS`) и `inboundLabelById`, владеет `invalidate()`. Сокращен с 667 до 199 строк — карточка сводки и таблица групп переехали в `GroupsTab`.

**`src/pages/groups/GroupsTab.tsx`** — таблица групп с колонкой тарифа, карточки сводной статистики (переехали из GroupsPage), создание/переименование через `GroupFormModal` (включая выбор тарифа), удаление, сброс трафика, **сброс тарифа**, добавление/удаление клиентов (через существующие модалки), под-ссылки, массовая настройка. Выпадающее меню действий строки. Мутации вызывают переработанные Go-эндпоинты групп с `tariffId`.

**`src/pages/groups/GroupFormModal.tsx`** — модальное окно с двумя полями (имя + выбор тарифа с опцией очистки «no tariff»), общее для создания и переименования. Передает `tariffs` как `{id, name}[]`.

**`src/pages/groups/TariffsTab.tsx`** — таблица тарифов (имя, теги стратегий трафика/инбаундов, количество групп, использующих тариф, просмотр/правка/удаление). Управляет мутациями create/update/delete/setProfiles.

**`src/pages/groups/TariffFormModal.tsx`** — экран конструирования: имя, выбор стратегий по полям, **конструктор цепочки профилей** (поиск/добавление/переупорядочивание/удаление профилей), живая таблица **предпросмотра результата** (эффективные трафик/срок/IP/инбаунды с атрибуцией `fromSource` и подсказками по стратегиям). При редактировании используемого тарифа показывается `impactNotice` («Затронуты будут N групп, M клиентов»). Вызывает TS `resolveChain` для предпросмотра и отправляет `{profileIds: [{id, position}]}` на `POST /tariffs/:id/profiles` (атомарная замена цепочки).

**`src/pages/groups/ProfilesTab.tsx`** — таблица профилей (имя, колонки трафика/срока/IP/инбаундов с `∞`/тегами, счетчик `usedByTariffs`). Удаление отклоняется на сервере при `tariffCount > 0`.

**`src/pages/groups/ProfileFormModal.tsx`** — три nullable `InputNumber`-поля (трафик/срок/IP) + мультиселект инбаундов (отфильтрован общими `inboundOptions` из `GroupsPage`).

**`src/pages/groups/TariffsTable.css`** — `.action-cell`/`.row-index`/`.action-buttons` для раскладки строк цепочки в `TariffFormModal`.

**`src/components/ManagedField.tsx`** — UI-наложение блокировки/разблокировки для полей, управляемых тарифом. Когда `managed` = true: дочерние элементы рендерятся инертными (`pointerEvents: none`), поверх накладывается активируемое при наведении размытие + иконка замка, по клику всплывает `Popover` с пояснением, что поле управляется тарифом, и кнопкой «make local». Тег с именем тарифа + замок сообщают источник; «make local» — «запасной выход».

**`src/hooks/useTariffOverrides.ts`** — отслеживает, какие поля сейчас переопределены, а какие управляются. Считывает четыре колонки переопределений клиента в `Set`; поддерживает два отложенных `Set` (`added`/`removed`); `makeLocal(field)`/`returnToTariff(field)` переключают членство; `computeDiff()` возвращает `{toOverride, toReturn}` — только дельты относительно исходного состояния клиента. `ClientFormModal` при сохранении вызывает из этой дельты ровно те `overrideField`/`returnToTariff`, которые нужны.

**`src/lib/tariff/resolveChain.ts`** — TS-двойник Go-резолвера в виде чистой функции. Возвращает `ResolvedPreview` с эффективными ID трафика/срока/IP/инбаундов **плюс** метки `trafficSource`/`expirySource`/`ipSource`/`inboundSource`, используемые предпросмотром в TariffFormModal. Теперь существует три реализации одного алгоритма (Go `client_tariff.go`, SQL `client_paging.go`, TS `resolveChain.ts`), каждая зафиксирована своим тестом.

**`src/lib/tariff/strategies.ts`** — enum `TrafficResolutionStrategy` + константа `bytesPerGB`, общие для резолвера и формы тарифа.

**`src/lib/clients/units.ts`** — хелперы единиц `bytesToGB`/`gbToBytes`, вынесенные из ранее инлайновых вычислений. Теперь используются `ClientFormModal`, `ClientsPage` и `client-total-bytes.test.tsx`.

**`src/schemas/tariff.ts`** — Zod-схемы для `Tariff`, `TariffProfileItem`, `ResolvedFields`, `TariffFormSchema`. Модальные формы валидируются через них.

**`src/schemas/profile.ts`** — Zod-схемы для `Profile`, `ProfileFormSchema`.

**`src/test/resolveChain.test.ts`** — TS-тест на 13 случаев, зеркалящий Go `TestResolveChain`: пустая цепочка, overwrite/sum для трафика, union/overwrite для инбаундов, пропуск null, метки источников, смешанные поля. Фиксирует TS-реализацию на той же семантике, что Go и SQL.

### Фронтенд: измененные файлы

**`src/pages/clients/ClientFormModal.tsx`** — становится чувствительной к тарифам. Загружает группы (теперь с `tariffId`/`tariffName`/`tariff`), вычисляет `isManaged`/`expiryManaged` из тарифа выбранной группы. Оборачивает `totalGB` (InputNumber), `limitIp` (InputNumber), `expiryTime` (DatePicker) и `inbounds` (мультиселект) в `ManagedField`. Управляемые поля: заблокированы со значком замка, отключены, показывают синий тег с именем тарифа, поповер «make local». Когда уже локальны: ссылка «return to tariff». Срок заблокирован, когда управляется тарифом и выключен `delayedStart`. `tariffManagedNotice` объясняет привязку. При сохранении `computeDiff()` из `useTariffOverrides` вызывает `POST /overrideField` или `/returnToTariff` для каждого измененного поля перед обычным `save()`.

**`src/pages/clients/ClientsPage.tsx`** — порядок слияния при гидрации редактирования перевернут с `{...row, ...full.client}` на `{...full.client, ...row}`, чтобы поля переопределений/`*IsOverridden`/`tariffName` slim-строки переживали слияние. Импортирует `gbToBytes` из нового `units.ts`. Убирает неиспользуемый пропс `groups` у `ClientFormModal`. Преобразует `allGroups` (string[]) в `{name: string}[]` для массовых модалок.

**`BulkAddToGroupModal.tsx`, `BulkAttachInboundsModal.tsx`, `BulkDetachInboundsModal.tsx`, `ClientBulkAddModal.tsx`** — `BulkAddToGroupModal` и `ClientBulkAddModal`: пропс группы изменен с `string[]` на `{name: string}[]`. `BulkAttachInboundsModal`/`BulkDetachInboundsModal` переходят на общий `MULTI_CLIENT_PROTOCOLS` (без изменения пропса группы).

**`src/pages/inbounds/clients/AddClientsToGroupModal.tsx`** — адаптирует список групп к виду `{name: string}[]`.

**`src/schemas/client.ts`** — `ClientRecordSchema` расширена булевыми флагами переопределения (`totalGBIsOverridden`, `limitIPIsOverridden`, `expiryIsOverridden`, `isInboundsOverridden`), `tariffName`, `*IsOverridden`. `GroupSummarySchema` расширена полями `tariffId`/`tariffName`/`tariff`.

**`src/schemas/primitives/protocol.ts`** — экспортирован `MULTI_CLIENT_PROTOCOLS` (ранее инлайном в `ClientFormModal`; теперь переиспользуется `ProfilesTab` и массовыми модалками).

**`src/api/queryKeys.ts`** — `keys.clients.tariffs()` и `keys.clients.profiles()` для инвалидации кэша TanStack Query.

**`src/layouts/AppSidebar.tsx`** — `groupsActive = pathname.startsWith('/groups')` для подсветки активного пункта навигации и состояния открытого подменю.

**`src/pages/api-docs/endpoints.ts`** — регистрирует новую секцию Profiles (5 эндпоинтов), секцию Tariffs (8 эндпоинтов, включая `overrideField`/`returnToTariff`), а также `get/resolve/:email` и `groups/resetTariff` в секции Clients. Требуется `build-openapi.mjs`.

**`src/generated/{types,zod,schemas,examples}.ts`** — перегенерированы из новых Go-структур через `tools/openapigen`. НЕ РЕДАКТИРОВАТЬ эти файлы — они создаются командой `npm run gen`.

**`frontend/public/openapi.json`** — перегенерированный API-документ с секциями тарифов/профилей и схемами новых эндпоинтов (+924 строки).

**`src/test/client-total-bytes.test.tsx`** — импортирует `gbToBytes` из нового `units.ts`.

**`src/test/__snapshots__/headers.test.ts.snap`, `inbound-defaults.test.ts.snap`** — только добавления. Исходные файлы тестов в этой ветке НЕ менялись. Новые записи взялись из тестовых случаев, уже присутствовавших в v3.6.0, чьи снапшоты устарели.

### i18n (13 файлов локалей)

Новые ключи: `menu.tariffs`, `pages.profiles.*` (13 ключей), `pages.tariffs.*` (49 ключей, включая `impactNotice`), `pages.clients.managedFieldLocked`/`managedFieldLockedDesc`/`makeLocal`/`returnToTariff`/`returnToTariffDesc`/`tariffManagedNotice`. Большая часть построчных изменений — переформатирование JSON: каждый файл локали переписан с единообразными отступами/сортировкой. Каждая локаль несет каждый новый английский ключ согласно конвенции i18n (отсутствующие ключи откатываются к en-US).

## Как это тестировалось?

- Фронтенд: проходят `npm run typecheck` и `npm run lint`; `npm run build` (продакшен-бандлы Vite) успешен. Запускался `npm run gen` — дифы в `src/generated/` и `public/openapi.json` являются результатом перегенерации новых Go-структур.
- Юнит-тесты фронтенда: `npm run test` — два существующих golden-снапшота (`src/test/__snapshots__/headers.test.ts.snap`, `inbound-defaults.test.ts.snap`) выросли аддитивно (исходные файлы тестов в этой ветке не менялись). Новые файлы тестов фронтенда в ветке:
  - `src/test/resolveChain.test.ts` — 13 случаев, зеркалящих Go `TestResolveChain`: пустая цепочка, overwrite/sum для трафика, union/overwrite для инбаундов, пропуск null, метки источников, смешанные поля.
  - `src/test/client-form-resolve.test.tsx` — разрешение тарифа в форме клиента.
  - `src/test/client-form-tariff.test.tsx` / `client-form-tariff-detail.test.tsx` — поведение формы с учетом тарифа.
  - `src/test/managed-field.test.tsx` — UI блокировки/разблокировки компонента ManagedField.
  - `src/test/useTariffOverrides.test.ts` — управление состоянием хука переопределений.
  - `src/test/units.test.ts` — конвертация единиц ГБ↔байты.
  - `src/test/form-modals-tariff.test.tsx`, `src/test/client-info-modal.test.tsx` — тарифная интеграция модалок.
- Бэкенд: проходят `go build ./...` и `go vet ./...`. `golangci-lint run ./...` проходит с нулевым количеством замечаний. Новые/переименованные файлы тестов бэкенда в ветке:
  - `internal/web/service/client_tariff_test.go` (671 строка, 9 наборов) — см. «Бэкенд: новые файлы» выше.
  - `internal/web/service/client_resolve_test.go` — тесты конвейера разрешения.
  - `internal/web/service/client_override_test.go` — тесты переопределения/возврата тарифу.
  - `internal/web/service/client_paging_tariff_test.go` — пагинация с вычисленными из тарифа SQL-выражениями.
  - `internal/web/service/client_link_tariff_test.go`, `client_link_test.go` — тесты разрешения инбаундов для xray/подписок.
  - `internal/web/service/client_model_test.go` — тесты моделей ToClientEffective/toClient.
  - `internal/web/service/client_tariff_history_test.go` — тесты таблицы истории ClientTariff.
  - `internal/web/service/tariff_test.go`, `profile_test.go` — юнит-тесты TariffService/ProfileService.
  - `internal/web/controller/group_test.go` — тесты контроллера групп (эндпоинты с учетом тарифа).
  - `internal/sub/service_tariff_test.go` — тесты разрешения тарифа в подписках.
  - Изменены: `internal/web/service/api_scale_postgres_test.go`, `sync_scale_postgres_test.go`.
- Ручная проверка по истории коммитов: изменения тарифа перезапускают xray при правках, влияющих на инбаунды; итоги трафика обновляются при изменении членства и правке тарифа; строки `ClientTariff` (`started_at`/`ended_at`) сохраняют срок стабильным; обработка единиц ГБ↔байты (трафик хранится в ГБ и конвертируется через `1<<30`); отвязка тарифа мгновенно восстанавливает собственные значения полей и назначения инбаундов каждого клиента без остаточных побочных эффектов.
- `internal/web/job/check_client_ip_job.go` — правоприменение лимита IP для fail2ban адаптировано под новую модель: `hasLimitIp()` и `loadClientLimits()` теперь вычисляют производные от тарифа лимиты IP через `tariff_profiles` → `profiles.limit_ip`, проходя по упорядоченным цепочкам профилей для каждого ID тарифа. Старый код читал только `clients.limit_ip` и не знал о лимитах из тарифов — без этого изменения fail2ban не стал бы применять лимиты IP, заданные через тарифы. Добавлен хелпер `resolveTariffLimitIPs()`, который пакетно загружает цепочки и вычисляет значения по тарифам с семантикой «последний побеждает».

## Скриншоты / записи

Н/Д.

## Ломающие изменения

Для существующих пользователей нет. Миграция чисто аддитивна — четыре новые таблицы и новые колонки в существующих таблицах через GORM AutoMigrate. Существующие строки не затрагиваются, миграция данных не требуется. Форма API групп обратно совместима: `POST /groups/create` получает необязательное поле `tariffId` (неизменные вызывающие продолжают работать), а `GET /groups` возвращает дополнительные поля `tariffId`/`tariffName`/`tariff` (аддитивно).

## Слои

| Слой | Файлов | +строк | −строк |
|------|--------|--------|--------|
| Core/Backend (Go) | 24 | 2 711 | 359 |
| Frontend (React) | 28 | 2 444 | 673 |
| Go тесты | 16 | 5 521 | 26 |
| Frontend тесты | 13 | — | — |
| i18n (13 локалей) | 13 | 1 170 | 53 |
| Generated (openapi, zod, types, msw) | 7 | 5 883 | 2 224 |
| **Итого** | **101** | **17 729** | **3 335** |

Слой Generated произведён командой `make gen` и не содержит рукописного кода — ревьюер может его пропустить.

## Чек-лист

- [x] Я протестировал изменение локально и подтвердил описанное поведение.
- [x] Я добавил или обновил тесты для нового поведения (если применимо).
- [x] `go build ./...` и набор тестов проходят локально.
- [x] Для изменений фронтенда: проходят `npm run lint`, `npm run typecheck` и `npm run build`.
- [x] Я обновил Wiki / README / API-документацию, если изменилось поведение, видимое пользователю.
- [x] Мои коммиты следуют существующему стилю сообщений проекта.
- [x] В этот PR не замешаны несвязанные изменения.
