# ClientFormModal / ClientInfoModal / Subscription — тестовый план

Ветка: `feat/group-tariff-profiles`. Сессия: `ses_036bd3b5cffej90iDh1zbzG2m1`.
Коммит: `d0fa7fb6` (все тесты T1-T12 + фиксы ревьюера в одном коммите).

---

## Контекст для следующей сессии

**Что уже сделано (в коммите):**
- T1-T12: 139+ тестов (Go + фронтенд), все зелёные
- Рецензент проверил — 0 багов, 0 ложных срабатываний
- Sub-пакет: 3 кейса покрыты (тариф без override, тариф с override, без тарифа)
- Controller: 21 тест (tariff/profile/group CRUD + rename/emails/resetTraffic/bulkAdd/bulkRemove/resetTariff)

**Что в staged (готово к коммиту):**
- `internal/sub/service_tariff_test.go` — TestAggregateTrafficByEmails_TariffWithOverrides

**Что осталось (P0 — НЕ сделано):**

### ClientFormModal (8 кейсов)
Код: `frontend/src/pages/clients/ClientFormModal.tsx` (1229 строк)
Ключевые точки: `keys.clients.groups()` для списка групп, `GET /panel/api/clients/get/resolve/:email?group=X` для резолва, `useTariffOverrides(client, isEdit, open)` для override-состояния, `ManagedField` для lock-иконок.

Инфраструктура для тестов:
- Мок групп: `queryClient.setQueryData(keys.clients.groups(), [{ name: 'vip', tariffId: 1, tariffName: 'Gold', tariff: { id: 1, name: 'Gold', trafficStrategy: 'overwrite', inboundStrategy: 'overwrite', enable: true } }])`
- Мок resolve API: нужен MSW (`server.use(http.get('*/clients/get/resolve/*', ...))`) или `vi.mock` на `HttpUtil.get`
- Мок save: `vi.fn().mockResolvedValue({ success: true })`
- Компонент рендерится через `ThemeProvider` + `QueryClientProvider` (см. `client-form-tariff.test.tsx` как пример)

Кейсы:
2.1 — Открыть, клиент в тарифе, без override → 4 lock-иконки, тарифные значения
2.2 — Открыть, клиент НЕ в тарифе → 0 lock, сырые значения
2.3 — Открыть, клиент в тарифе, totalGB overridden → 3 lock, totalGB без lock
2.4 — Очистить поле группы → lock исчезают, значения → сырые
2.5 — Выбрать группу с тарифом → lock появляются, значения из API
2.6 — Сохранить: клиент в тарифе → totalGBMode="tariff"
2.7 — Сохранить: клиент без тарифа → totalGBMode="own"
2.8 — Сохранить: клиент с override → totalGBMode="override"

### ClientInfoModal (3 кейса)
Код: `frontend/src/pages/clients/ClientInfoModal.tsx`
Данные: `client` prop = `ClientRecord` из paged-list (УЖЕ тарифно-разрешённые через ClientBatchResolver).
Тестируется чистыми props — без API-моков.

Кейсы:
3.1 — Клиент в тарифе, без override → effective значения
3.2 — Клиент в тарифе, с override → микс
3.3 — Клиент без тарифа → сырые значения

### ИСКЛЮЧЕНО из плана
- InboundInfoModal: только single-user инбаунды (socks/http/mixed/tunnel/tun/dokodemo), показывает RAW значения, не тарифные
- BulkAddToGroupModal: тонкая форма, логика на бэкенде (уже покрыта Go-тестами)
- QR/Share ссылки: логика в sub-пакете (уже покрыта T6)

---

## 0. Где хранится переопределённое значение

Переопределённое значение в ТОЙ ЖЕ колонке что и собственное клиента: `total_gb`, `limit_ip`, `expiry_time`.
Флаг `is_*_overridden = true` говорит резолверу «читай из колонки клиента, игнорируй тариф».
Нет отдельных `total_gb_override` колонок.
Inbounds: `is_inbounds_overridden` — флаг, сами inbound_ids хранятся в `client_inbounds` (read-only для тарифа).

---

## 1. Sub-пакет (AggregateTrafficByEmails) — ✅ ГОТОВО

| # | Кейс | Статус | Тест |
|---|------|--------|------|
| 1.1 | Клиент в тарифе, без override | ✅ | TestAggregateTrafficByEmails_TariffEffectiveLimits |
| 1.2 | Клиент в тарифе, с override (totalGB) | ✅ | TestAggregateTrafficByEmails_TariffWithOverrides |
| 1.3 | Клиент без тарифа | ✅ | TestAggregateTrafficByEmails_FallsBackToClientLimits (мейнтейнер) + _NoTariff_KeepClientLimits |

---

## 2. ClientFormModal (edit-форма клиента)

Код: `frontend/src/pages/clients/ClientFormModal.tsx` (1229 строк)

Ключевой поток: открытие → отображение полей → смена группы → сохранение.
Группы загружаются через `useQuery({ queryKey: keys.clients.groups() })`.
Resolve при смене группы: `GET /panel/api/clients/get/resolve/:email?group=X`.
Override-состояние: хук `useTariffOverrides(client, isEdit, open)`.
Значения полей: `ManagedField` оборачивает каждое поле, показывая lock/blur когда managed.

| # | Кейс | Что проверяем | Как проверить |
|---|------|--------------|---------------|
| 2.1 | Открыть, клиент в тарифе, без override | Все 4 поля заблокированы (ManagedField с lock-иконкой), значения тарифные | `querySelectorAll('.anticon-lock').length === 4` |
| 2.2 | Открыть, клиент НЕ в тарифе | lock=0, значения — сырые клиентские | lock-иконок = 0 |
| 2.3 | Открыть, клиент в тарифе, totalGB overridden | totalGB без lock, limitIP/expiry/inbounds с lock. totalGB = overridden значение клиента | lock на totalGB нет, на остальных есть |
| 2.4 | Очистить поле группы | Все lock исчезают, значения → сырые клиентские | Меняем Select группы на пустой |
| 2.5 | Выбрать группу с тарифом | lock появляются, значения резолвятся через API resolve | Меняем Select → ждём API → проверка |
| 2.6 | Сохранение: клиент в тарифе | totalGBMode="tariff" в payload | Мок save, проверка полей |
| 2.7 | Сохранение: клиент без тарифа | totalGBMode="own" | Мок save |
| 2.8 | Сохранение: клиент с override | totalGBMode="override" для переопределённых, "tariff" для остальных | Мок save |

**Инфраструктура:** `queryClient.setQueryData(keys.clients.groups(), [...])` для мока групп.
Для resolve API нужен либо MSW, либо `vi.mock` на HttpUtil.

---

## 3. ClientInfoModal (инфо-модалка клиента)

**Код:** `frontend/src/pages/clients/ClientInfoModal.tsx`
**Данные:** `client` prop = `ClientRecord` из `useClients()` = paged-list (`GET /panel/api/clients/list/paged`).
**Paged-list возвращает ТАРИФНО-РАЗРЕШЁННЫЕ значения** — `ClientBatchResolver.ResolveLimits` для totalGB/expiry/limitIP,
`resolveEffectiveInboundsForPage` для inboundIds. Клиент в тарифе → effective значения в модалке.
**НЕТ** отдельных API-вызовов внутри модалки для резолва. Всё уже в `client` prop.
**Существующие тесты:** 0.

| # | Кейс | Что проверяем |
|---|------|--------------|
| 3.1 | Клиент в тарифе, без override | totalGB/expiry/limitIP/inbounds = тарифные значения |
| 3.2 | Клиент в тарифе, с override (totalGB) | totalGB = overridden клиентское, остальные = тарифные |
| 3.3 | Клиент без тарифа | Все значения = сырые клиентские |

**Инфраструктура:** передать `ClientRecord` с тарифно-разрешёнными полями как prop, проверить DOM.
Не нужны API-моки — чистый render + props.

---

## 4. Результаты изучения кода

### 4.1 QR/share-link — НЕ в ClientFormModal

**Факт:** `ClientFormModal` НЕ имеет вкладки с QR/share-ссылками. Вкладка "links" — только external links.
QR/Share-ссылки живут в:
- `ClientQrModal.tsx` — QR-only модалка со страницы клиентов
- `ClientInfoModal.tsx` — вкладка с SUB/JSON/CLASH ссылками

Обе получают ссылки через `GET /panel/api/clients/subLinks/${subId}`.
Sub-сервис резолвит effective inbound set через `ResolveClientFields(...).InboundIds`.
**Вывод:** ссылки генерируются для тарифно-разрешённых инбаундов — тестировать это на уровне sub-пакета (уже покрыто).

### 4.2 BulkAddToGroup — backend подтверждён

**Фронт:** `BulkAddToGroupModal` — тонкая форма, без своей логики. `ClientsPage` вызывает `bulkAddToGroup` из `useClients` → `POST /panel/api/clients/groups/bulkAdd`.
**Бэкенд:** `GroupController.bulkAdd` → `ClientService.AddToGroup`:
- Обновляет `group_name` для emails
- Если группа имеет тариф: вызывает `resetClientOverridesByEmails` → очищает 4 флага + создаёт `ClientTariff` строки
- Затем `RefreshTrafficForGroup` → переписывает `client_traffics`
- Ставит `xrayService.SetToNeedRestart()`

**Вывод:** bulkAdd в тарифную группу корректно создаёт ClientTariff и сбрасывает overrides.
Go-тесты покрывают этот путь (`group_test.go`, `tariff_test.go`). Фронтенд-тест для модалки не критичен.

### 4.3 InboundInfoModal — СЫРЫЕ значения, НЕ тарифные

**Код:** `frontend/src/pages/inbounds/info/InboundInfoModal.tsx`
**Данные:** парсит сырой `settings.clients` JSON из `dbInbound` (inbound config, НЕ paged-list).
**НЕ применяет тарифный резолвинг.** Показывает `clientSettings.totalGB`, `clientSettings.expiryTime` —
это значения из JSON-конфига инбаунда, без учёта тарифа.
`client_traffics` кэш может быть обновлён через `RefreshTrafficForGroup`, но сама модалка не резолвит.

**КЛЮЧЕВОЕ РАСХОЖДЕНИЕ:** `ClientInfoModal` показывает effective значения (из paged-list),
`InboundInfoModal` показывает **raw** значения (из settings JSON). Разное поведение для одного и того же клиента.

**Вывод:** тестировать НЕ тарифное поведение — модалка показывает raw. Если ожидается что она ДОЛЖНА показывать effective — это баг, не тест.

**Существующие тесты:** 0.

---

## 5. Итоговый приоритет

| Приоритет | Область | Почему | Сложность |
|-----------|---------|--------|-----------|
| 🔴 P0 | ClientFormModal 2.1-2.8 | Основной путь редактирования, больше всего багов | Высокая (нужен mock resolve API) |
| 🟡 P1 | ClientInfoModal 3.1-3.3 | Просмотр effective значений, без API-моков | Низкая (pure props) |
| ⚪ P2 | InboundInfoModal | Только raw значения — тестировать нечего, если это не баг | Низкая |
| ⚪ P2 | BulkAddToGroupModal | Тонкая форма, логика на бэкенде (уже покрыто) | Не нужно |
| ⚪ P2 | QR/Share ссылки | Логика в sub-пакете (уже покрыто T6) | Не нужно |
