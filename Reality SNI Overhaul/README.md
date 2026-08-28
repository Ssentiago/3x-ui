# Reality SNI Overhaul

## Суть фичи

Валидация Reality SNI таргетов при добавлении. Панель проверяет что таргет поддерживает TLS 1.3 + H2 — обязательные требования Reality протокола.

## Фичи

1. **Validation API** — endpoint проверки TLS 1.3 + H2 для любого таргета
2. **Validation UI** — кнопка рядом с полем SNI / автовалидация при добавлении

## Ключевые решения

- **Protocol check, не reachability** — проверяем что таргет поддерживает TLS 1.3 + H2, а не доступен ли он. Split-brain не влияет на protocol check.
- **Нулевая зависимость** — никаких внешних библиотек, просто TCP + TLS handshake.

## Порядок реализации

### Этап 1: Validation функция
- [ ] `ValidateRealityTarget` — TCP + TLS 1.3 + H2 + cert check
- [ ] `ValidationResult` — структура с результатами

### Этап 2: API endpoint
- [ ] `POST /panel/api/server/validateRealityTarget`
- [ ] Request: `{ target, sni }`
- [ ] Response: `{ ok, tls13, h2, certValid, certExpiry, error }`

### Этап 3: Frontend
- [ ] Кнопка "Validate" рядом с полем Reality target/SNI
- [ ] Или автовалидация при добавлении в список
- [ ] Показ результата: ✅ valid / ❌ invalid + причина

### Этап 4: Тесты
- [ ] Unit tests: валидация с mock TLS
- [ ] Manual E2E: добавить таргет → нажать validate → получить результат
