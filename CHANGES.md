# Changes in feat/notifications-eventbus

## Overview

Added event bus architecture for notifications with Telegram bot and Email (SMTP) subscribers.

## New Files

### Event Bus Core
- **`internal/eventbus/bus.go`** — Event bus implementation with buffered channel, fan-out, panic recovery. Supports Subscribe/Unsubscribe/Publish pattern.
- **`internal/eventbus/bus_test.go`** — 7 unit tests for event bus core functionality.
- **`internal/eventbus/events.go`** — Event type definitions (outbound.down/up, xray.crash, cpu.high, login.attempt) and data structs (OutboundHealthData, LoginEventData).

### Email Notifications
- **`internal/web/service/email/email.go`** — SMTP email service with TLS/STARTTLS support, 30s timeout, TestConnection() with stage-by-stage testing (connect/auth/send), classifySMTPError() for human-readable errors.
- **`internal/web/service/email/subscriber.go`** — Email event bus subscriber with rate limiting, Xray state tracking to prevent duplicates, login events bypass rate limiter.
- **`internal/web/service/email/ratelimiter_test.go`** — 3 unit tests for rate limiter.

### Telegram Notifications
- **`internal/web/service/tgbot/tgbot_event.go`** — Telegram event bus subscriber with HTML formatting, rate limiting, Xray state tracking, login events bypass rate limiter.

### Frontend
- **`frontend/src/components/ui/EventBusCheckboxes.tsx`** — Reusable UI component for event selection with collapsible groups, inline CPU threshold input.
- **`frontend/src/pages/settings/EmailTab.tsx`** — Email settings tab with SMTP configuration, encryption selector (none/STARTTLS/TLS), test button with stage reporting.

## Modified Files

### Backend - Event Publishing

#### `internal/xray/process.go`
- Added `OnCrash func(err error)` callback field
- Calls OnCrash when Xray process fails unexpectedly (line 571-573)

#### `internal/web/web.go`
- Initializes event bus: `s.bus = eventbus.New(eventbus.DefaultBufferSize)`
- Registers email subscriber: `s.bus.Subscribe("email-notifier", emailSub.HandleEvent)`
- Registers Telegram subscriber: `s.bus.Subscribe("tg-notifier", tgbotSub.HandleEvent)`
- Wires Xray crash callback: `xray.OnCrash = func(err error) { ... }`

#### `internal/web/service/xray_metrics.go`
- Publishes `EventOutboundDown` when outbound transitions from alive to dead
- Publishes `EventOutboundUp` when outbound transitions from dead to alive
- Uses observatory data for health monitoring

#### `internal/web/job/check_cpu_usage.go`
- Added event bus import
- Changed from direct `SendMsgToTgbotAdmins()` to `EventBus.Publish(EventCPUHigh)`
- Added `EventBus` global variable

#### `internal/web/job/check_xray_running_job.go`
- Added `EventBus` global variable (used by other jobs)

#### `internal/web/service/tgbot/tgbot_report.go`
- Modified `UserLoginNotify()` to publish `EventLoginAttempt` to event bus instead of direct Telegram send
- Removed `tgBotLoginNotify` check — login notifications always work when bot is enabled

### Backend - Settings & API

#### `internal/web/entity/entity.go`
- Removed: `TgBotProxy`, `TgBotLoginNotify`
- Added: `SmtpEnabledEvents string`, `SmtpCpu int`, `SmtpEncryptionType string`

#### `internal/web/service/setting.go`
- Added to `defaultValueMap`: `smtpEnabledEvents`, `smtpCpu`, `smtpEncryptionType`
- Added getters/setters: `GetSmtpEnabledEvents/SetSmtpEnabledEvents`, `GetSmtpCpu/SetSmtpCpu`, `GetSmtpEncryptionType/SetSmtpEncryptionType`, `GetTgEnabledEvents/SetTgEnabledEvents`

#### `internal/web/controller/setting.go`
- Added `testSmtp` endpoint: `POST /setting/testSmtp` — tests SMTP connection with stage reporting
- Added `testTgBot` endpoint: `POST /setting/testTgBot` — tests Telegram bot connection
- Added `SetEmailService()` function to inject email service

#### `internal/web/service/tgbot/tgbot.go`
- Added `EventBus` global variable for event publishing

#### `internal/web/service/tgbot/tgbot_send.go`
- Added `TestConnection()` method for Telegram bot testing

### Frontend - Settings

#### `frontend/src/models/setting.ts`
- Removed: `tgBotProxy`, `tgBotLoginNotify`
- Added: `smtpEnabledEvents`, `smtpCpu`, `smtpEncryptionType`, `tgEnabledEvents`

#### `frontend/src/pages/settings/TelegramTab.tsx`
- Replaced individual login/CPU toggles with `EventBusCheckboxes` component
- Added test button for Telegram bot
- Added inline CPU threshold input in event checkboxes

#### `frontend/src/pages/settings/SettingsPage.tsx`
- Added `EmailTab` route

#### `frontend/src/layouts/AppSidebar.tsx`
- Added "Email" menu item in settings

#### `frontend/src/components/ui/index.ts`
- Exported `EventBusCheckboxes` component

### i18n (13 locale files)

#### `internal/web/translation/en-US.json`
Added keys:
- `tgbot.messages.eventOutboundDown/Up` — outbound status notifications
- `tgbot.messages.eventXrayCrash/Error` — xray crash notifications
- `tgbot.messages.eventCPUHigh/Detail` — CPU threshold notifications
- `tgbot.messages.eventLoginFallback` — login fallback message
- `email.*` — SMTP labels, statuses, subjects, titles
- `pages.settings.eventGroup*` — event group labels
- `pages.settings.event*` — individual event labels
- `pages.settings.smtp*` — SMTP settings labels
- `pages.settings.smtpEncryption*` — encryption type labels
- `pages.settings.smtpStage*` — test stage labels
- `pages.settings.smtpTestSuccess` — test success message

#### `internal/web/translation/ru-RU.json`
Same keys with Russian translations. Notable translations:
- "Outbound" → "Исходящий"
- "Node" → "Узел"
- "Xray Core" → "Ядро Xray"
- "CPU high" → "Превышение порога CPU"
- "Login attempt" → "Попытка входа"

## Event Types

| Event | Trigger | Rate Limited |
|-------|---------|--------------|
| `outbound.down` | Observatory detects outbound dead | Yes (1 min) |
| `outbound.up` | Observatory detects outbound alive | Yes (1 min) |
| `xray.crash` | OnCrash callback fires | State tracking (1 notification) |
| `cpu.high` | CPU threshold exceeded | Yes (1 min) |
| `login.attempt` | Login success/failure | No (always works) |

## Key Behaviors

1. **Login notifications** — Always work when bot/email is enabled, bypass `isEventEnabled` check and rate limiter
2. **Xray crash** — Only notifies once per crash (state tracking prevents duplicates from crash + down events)
3. **CPU threshold** — Single notification per threshold breach, rate limited to 1 per minute
4. **SMTP test** — Stage-by-stage testing (connect → auth → send) with human-readable error messages
5. **Encryption types** — None (plain), STARTTLS, TLS (implicit) — user selects in settings
