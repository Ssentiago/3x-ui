## 1. Тесты: own/tariff/override флоу

### Backend (internal/web/service/)

**Update с mode-флагами** (`client_crud_test.go`):
- `"tariff"` → существующее значение сохраняется, не перезаписывается
- `"override"` → значение пишется, `is_*_overridden` флаг ставится
- `"own"` → обычная запись
- Смешанные флаги (totalGB=tariff, limitIP=override, expiryTime=own)
- `"tariff"` когда поле уже overridden (`is_*_overridden` = true) → НЕ должно пропускать, должно писать
- Краевой: `"tariff"` но клиент не в тарифной группе → флаг игнорируется

**ResolveClientFields с override** (`client_tariff_test.go`):
- Создать клиент+тариф+группу → resolve возвращает тарифные значения
- Override одного поля → resolve возвращает overridden значение для поля, тарифные для остальных
- returnToTariff → resolve снова возвращает тарифные значения
- Негативный expiryTime (тариф с днями, tariffStartedAt=null, зашли/не зашли в тариф)

**resetClientOverrides** (`client_tariff_test.go`):
- Клиент входит в тарифную группу → все 4 override-флага сброшены, создана запись `client_tariffs`
- Клиент меняет группу на другой тариф → старый `ClientTariff` завершён, новый создан, флаги сброшены

### Frontend (frontend/src/)

**useTariffOverrides hook** (новый `useTariffOverrides.test.ts`):
- Начальное состояние: клиент без overrides → `isFieldManaged(f)` = true для всех полей
- `makeLocal('totalGB')` → `isFieldManaged('totalGB')` = false, `computeDiff().toOverride` = ['totalGB']
- `returnToTariff('totalGB')` → возвращает в managed, `computeDiff().toReturn` = ['totalGB']
- `computeDiff()` при нескольких мутациях: 2 makeLocal, 1 returnToTariff → корректные списки
- Сброс состояния при закрытии модалки (`open=false`)

### Регрессионные (отыгрыш багов из fix-коммитов)
- `fix(tariff): prevent tariff values from being written to client records` — тест: после сохранения клиента с режимом `tariff`, в client_records не попали тарифные значения
- `fix(tariff): always show Date picker for tariff-managed expiry` — тест: resolve с tariffStartedAt=null не переключает на Days
- `fix(tariff): reorder resolve/effective routes` — тест: `/get/resolve/:email` и `/get/effective/:email` работают корректно
- `fix(tariff): resolveForClient accepts db param` — тест: не падает при nil db

## 2. Резолвинг полей в подписке

Та же проблема что с формой клиента — подписка должна резолвить тарифные значения (totalGB, expiryTime, limitIP) через `ResolveClientFields`. Проверить `internal/sub/` и `client_link.go`.
