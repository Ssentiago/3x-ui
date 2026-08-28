## План: тотальная консолидация резолвинга клиента

### Проблема

Резолвинг полей клиента (totalGB, expiryTime, limitIP, inboundIds) через тариф
размазан по N местам с параллельными, неполными, багованными реализациями.
Как следствие — подписка, форма, инфо показывают разные значения.

### Потребители данных клиента (требуют тарифного резолвинга)

#### Слой «панель» (уже используют или должны использовать ResolveClientFields)

| # | Где | Файл | Что читает | Резолвинг? |
|---|-----|------|-----------|-----------|
| 1 | Страница клиентов (таблица) | `client_paging.go:buildPage` | totalGB, expiryTime, limitIP | ✅ через `effectiveTotalGB/ExpiryTime/LimitIP` |
| 2 | Форма создания/редакт. клиента | `ClientFormModal.tsx` | totalGB, expiryTime, limitIP, inboundIds | формы — через resolve-API, submit — mode-флаги |
| 3 | Инфо-модалка клиента | `ClientInfoModal.tsx` | totalGB, expiryTime, limitIP | ❓ зависит от API-эндпоинта |
| 4 | GET /get/effective/:email | `controller/client.go:getEffective` | все поля | ✅ через `ResolveClientFields` |
| 5 | GET /get/resolve/:email | `client_lookup.go:ResolveForGroup` | totalGB, expiryTime, limitIP, inboundIds | ✅ через `ResolveClientFields` |
| 6 | Инфо вкладка inbound | `InboundInfoModal.tsx` | totalGB/expiryTime/limitIp из `settings.clients[]` JSON | ❌ читает сырые настройки Xray |
| 7 | Колонки inbound (traffic) | `useInboundColumns.tsx` | expiryTime из traffic-строк | ❌ читает кеш трафика |

#### Слой «подписка» (sub)

| # | Где | Файл | Что читает | Резолвинг? |
|---|-----|------|-----------|-----------|
| 8 | Список клиентов для ссылок | `client_link.go:listForInboundFiltered` | totalGB, expiryTime, limitIP | ⚠️ есть своя реализация, была с багом (починена) |
| 9 | Инфо в подписке | `sub/service.go:subInfoMap` | totalGB, expiryTime | ⚠️ своя реализация, через `startedAtMap` |
| 10 | Переменные ремарков | `sub/remark_vars.go:statsForClient` | totalGB, expiryTime | ⚠️ читает `model.Client` (может быть неразрешённым) |

#### Слой «телеграм-бот»

| # | Где | Файл | Что читает | Резолвинг? |
|---|-----|------|-----------|-----------|
| 11 | Инфо клиента в боте | `tgbot/tgbot_client.go:formatClientInfo` | totalGB, expiryTime | ❌ читает `xray.ClientTraffic` (кеш) |
| 12 | Отчёты бота | `tgbot/tgbot_report.go` | totalGB, expiryTime | ❌ читает `inbound.ClientStats[]` (кеш) |

### Корень проблемы

`ResolveClientFields` — ОДНА функция, которая делает всё правильно.
Но код в `client_link.go` и `sub/service.go` обходит её и реализует
свой параллельный резолвинг (сбор chainCache, startedAtMap, ручной
перебор override-флагов). Отсюда баги и расхождения.

### Решение

**Единственный источник истины для резолвинга — `ResolveClientFields`.**

Что делаем:
1. Все потребители, которые читают `totalGB`/`expiryTime`/`limitIP` клиента,
   должны проходить через `ResolveClientFields` (или `ResolveClientLimits`).
2. Никаких ручных `chainCache`/`startedAtMap`/поштучного перебора override-флагов.
3. Там где нужна батч-производительность — вызывать `ResolveClientFields` один раз
   для КАЖДОГО клиента (это быстро, там один SQL на клиента максимум, да и кеширование
   можно добавить внутри самой функции).

### Конкретные шаги

1. ❌ `client_link.go:listForInboundFiltered` (строки 315-410) —
   выкинуть ручную реализацию chainCache/startedAtMap, заменить на
   `ResolveClientFields` для каждого клиента из списка `unique`.

2. ❌ `sub/service.go:subInfoMap` — аналогично, заменить ручную реализацию
   на `ResolveClientFields`.

3. ❌ `sub/remark_vars.go:statsForClient` — убедиться что `client` параметр
   приходит уже tariff-resolved (если caller — `listForInboundFiltered`).

4. ⚠️ Фронтенд `InboundInfoModal.tsx` — читает `settings.clients[]` JSON (сырые настройки Xray),
   а не `ClientRecord`. Нужно либо подтянуть tariff-resolved значения отдельным запросом,
   либо в API добавить их в ответ.

5. ⚠️ Telegram бот — читает `xray.ClientTraffic` (кеш трафика). Для `totalGB`/`expiryTime`
   нужно читать из `ClientRecord` через `ResolveClientFields`.

### Ожидаемый результат

После всех замен:
- Панель: ✅ (уже ок, `client_paging.go` + getEffective)
- Форма клиента: ✅ (resolve-API)
- Подписка: ✅ (через ResolveClientFields)
- Инфо inbound: ✅ (исправлено)
- Telegram бот: ✅ (исправлено)
