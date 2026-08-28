# Spec: Group / Tariff / Profile

**На основе:** `design-group-tariff-profile.md`
**Ветка:** `feat/group-tariff-profiles`
**Дата:** 2026-07-31

---

## 1. Go-модель

### 1.1. Новые struct-ы — `internal/database/model/profile.go`

```go
package model

type Profile struct {
    Id         int    `json:"id" gorm:"primaryKey;autoIncrement" example:"1"`
    Name       string `json:"name" gorm:"uniqueIndex;not null" example:"BASE"`
    Traffic    *int64 `json:"traffic" gorm:"column:traffic" example:"100"`
    ExpiryDays *int   `json:"expiryDays" gorm:"column:expiry_days" example:"30"`
    LimitIP    *int   `json:"limitIp" gorm:"column:limit_ip" example:"3"`
    InboundIds string `json:"inboundIds" gorm:"column:inbound_ids;default:null" example:"[1,2]"`
    CreatedAt  int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
    UpdatedAt  int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (Profile) TableName() string { return "profiles" }

type TariffProfile struct {
    TariffID  int `json:"tariffId" gorm:"primaryKey;column:tariff_id"`
    ProfileID int `json:"profileId" gorm:"primaryKey;column:profile_id"`
    Position  int `json:"position" gorm:"uniqueIndex:idx_tariff_position;column:position;not null;default:0"`
}

func (TariffProfile) TableName() string { return "tariff_profiles" }
```

**Семантика nullable:** `Traffic/ExpiryDays/LimitIP *T` — nil = профиль не высказывается по этому полю (skip при наложении). `InboundIds` — TEXT, `nil`-строка от GORM = поле не управляется. Пустая строка `"[]"` — явный пустой список.

### 1.2. Изменение `Tariff` — `internal/database/model/model.go`

**Было (текущее):**
```go
type Tariff struct {
    Id         int    `json:"id" gorm:"primaryKey;autoIncrement" example:"1"`
    Name       string `json:"name" gorm:"uniqueIndex;not null" example:"Gold"`
    InboundIds string `json:"inboundIds" gorm:"column:inbound_ids;default:'[]'" example:"[1,2]"`
    TotalGB    int64  `json:"totalGB" gorm:"column:total_gb;default:0" example:"100"`
    ExpiryDays int    `json:"expiryDays" gorm:"column:expiry_days;default:0" example:"30"`
    LimitIP    int    `json:"limitIp" gorm:"column:limit_ip;default:0" example:"3"`
    Enable     bool   `json:"enable" gorm:"default:true" example:"true"`
    CreatedAt  int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
    UpdatedAt  int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}
```

**Стало:**
```go
type Tariff struct {
    Id              int    `json:"id" gorm:"primaryKey;autoIncrement" example:"1"`
    Name            string `json:"name" gorm:"uniqueIndex;not null" example:"Gold"`
    TrafficStrategy string `json:"trafficStrategy" gorm:"column:traffic_strategy;not null;default:overwrite" example:"sum"`
    InboundStrategy string `json:"inboundStrategy" gorm:"column:inbound_strategy;not null;default:overwrite" example:"union"`
    Enable          bool   `json:"enable" gorm:"default:true" example:"true"`
    CreatedAt       int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
    UpdatedAt       int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}
```

Стратегии — строковые константы. Пакет декларирует:
```go
const (
    StrategyOverwrite = "overwrite"
    StrategySum       = "sum"
    StrategyUnion     = "union"
)
```

`InboundIds`, `TotalGB`, `ExpiryDays`, `LimitIP` — **удалены** из struct-а. В БД-колонки остаются, GORM их игнорирует (нет тегов).

### 1.3. `ClientRecord` — без изменений

`TotalGBOverride *int64`, `LimitIPOverride *int`, `ExpiryTimeOverride *int64`, `InboundsOverride *int`, `TariffStartedAt *int64` — остаются как есть.

### 1.4. `ClientGroup` — без изменений

`TariffID *int` — остаётся.

### 1.5. Регистрация в AutoMigrate

В `internal/database/db.go`, в `allModels()`:
```go
func allModels() []any {
    return []any{
        // ... существующие ...
        &model.Profile{},
        &model.TariffProfile{},
        // ... остальные ...
    }
}
```

Без data-миграции — фича не в проде, существующих тарифов/клиентов с tariff-привязками нет. Старые колонки `tariffs.total_gb, expiry_days, limit_ip, inbound_ids` остаются в БД — GORM их игнорирует (нет тегов в struct-е).

---

## 2. Резолвер

### 2.1. Go-резолвер — `internal/web/service/client_tariff.go`

Переписывается полностью. Оставляет только `OverrideField`, `ReturnToTariff`, `lookupTariffForGroup`.

#### Сигнатуры

```go
type ClientTariffService struct{}

const bytesPerGB = 1 << 30

type tariffContext struct {
    Tariff   *model.Tariff
    Profiles []model.Profile
}

func (s *ClientTariffService) resolveForClient(client *model.ClientRecord) (*tariffContext, error)
func resolveChain(ctx *tariffContext) *EffectiveConfig
func resolveOverrides(client *model.ClientRecord, chain *EffectiveConfig) *model.Client
```

#### `resolveForClient`

```go
func (s *ClientTariffService) resolveForClient(client *model.ClientRecord) (*tariffContext, error) {
    if client.Group == "" {
        return nil, nil
    }
    db := database.GetDB()
    var grp model.ClientGroup
    if err := db.Where("name = ?", client.Group).First(&grp).Error; err != nil {
        if !errors.Is(err, gorm.ErrRecordNotFound) {
            logger.Warningf("resolveForClient: group lookup failed for %s: %v", client.Email, err)
        }
        return nil, nil
    }
    if grp.TariffID == nil {
        return nil, nil
    }
    var tariff model.Tariff
    if err := db.First(&tariff, *grp.TariffID).Error; err != nil {
        if !errors.Is(err, gorm.ErrRecordNotFound) {
            logger.Warningf("resolveForClient: tariff lookup failed for %s (tariffId=%d): %v", client.Email, *grp.TariffID, err)
        }
        return nil, nil
    }
    var profiles []model.Profile
    db.Table("profiles").
        Joins("JOIN tariff_profiles ON tariff_profiles.profile_id = profiles.id").
        Where("tariff_profiles.tariff_id = ?", tariff.Id).
        Order("tariff_profiles.position ASC").
        Find(&profiles)
    return &tariffContext{Tariff: &tariff, Profiles: profiles}, nil
}
```

#### EffectiveConfig

```go
type EffectiveConfig struct {
    Traffic    int64   // bytes
    ExpiryDays int     // relative days from chain resolution
    LimitIP    int
    InboundIds []int
}
```

#### Алгоритм resolveChain

```go
func resolveChain(ctx *tariffContext) *EffectiveConfig {
    cfg := &EffectiveConfig{}
    for _, p := range ctx.Profiles {
        if p.Traffic != nil {
            applyTrafficNumeric(&cfg.Traffic, int64(*p.Traffic)*bytesPerGB, ctx.Tariff.TrafficStrategy)
        }
        if p.ExpiryDays != nil {
            cfg.ExpiryDays = *p.ExpiryDays
        }
        if p.LimitIP != nil {
            cfg.LimitIP = *p.LimitIP
        }
        if p.InboundIds != "" && p.InboundIds != "null" {
            ids, _ := parseInboundIds(p.InboundIds)
            applyInboundIds(&cfg.InboundIds, ids, ctx.Tariff.InboundStrategy)
        }
    }
    return cfg
}

func applyTrafficNumeric(target *int64, val int64, strategy string) {
    switch strategy {
    case StrategySum:
        *target += val
    default: // overwrite
        *target = val
    }
}

func applyInboundIds(target *[]int, ids []int, strategy string) {
    switch strategy {
    case StrategyUnion:
        seen := make(map[int]struct{})
        for _, id := range *target {
            seen[id] = struct{}{}
        }
        for _, id := range ids {
            if _, ok := seen[id]; !ok {
                *target = append(*target, id)
                seen[id] = struct{}{}
            }
        }
    default: // overwrite
        *target = append([]int(nil), ids...)
    }
}
```

#### `resolveOverrides` — финальный шаг

```go
func resolveOverrides(client *model.ClientRecord, ctx *tariffContext) *model.Client {
    var chain *EffectiveConfig
    if ctx != nil {
        chain = resolveChain(ctx)
    }
    effectiveTotalGB := client.TotalGB
    effectiveLimitIP := client.LimitIP
    effectiveExpiryTime := client.ExpiryTime
    if client.TotalGBOverride != nil {
        effectiveTotalGB = *client.TotalGBOverride
    } else if chain != nil {
        effectiveTotalGB = chain.Traffic
    }
    if client.LimitIPOverride != nil {
        effectiveLimitIP = *client.LimitIPOverride
    } else if chain != nil {
        effectiveLimitIP = chain.LimitIP
    }
    if client.ExpiryTimeOverride != nil {
        effectiveExpiryTime = *client.ExpiryTimeOverride
    } else if chain != nil && chain.ExpiryDays > 0 && client.TariffStartedAt != nil {
        effectiveExpiryTime = *client.TariffStartedAt + int64(chain.ExpiryDays)*86400*1000
    }
    return client.ToClientEffective(effectiveLimitIP, effectiveTotalGB, effectiveExpiryTime)
}
```

#### Публичные функции — замены текущих

```go
func ToClientEffective(rec *model.ClientRecord) *model.Client {
    var s ClientTariffService
    ctx, _ := s.resolveForClient(rec)
    return resolveOverrides(rec, ctx)
}

func EffectiveTotalGB(client *model.ClientRecord) int64 { ... }
func EffectiveLimitIP(client *model.ClientRecord) int    { ... }
func EffectiveExpiryTime(client *model.ClientRecord) int64 { ... }
```

Сигнатуры без параметра `tariff` — резолвят сами.

### 2.2. SQL-резолвер — `internal/web/service/client_paging.go`

Текущие выражения:
```go
sqlEffTotalGB = "COALESCE(c.total_gb_override, trf.total_gb << 30, c.total_gb)"
sqlEffExpiry  = "CASE WHEN ... c.tariff_started_at + trf.expiry_days * 86400000 ... END"
sqlEffLimitIP = "COALESCE(c.limit_ip_override, trf.limit_ip, c.limit_ip)"
```

Замена на подзапросы через `tariff_profiles` + `profiles`. Три SQL-выражения.

#### `sqlEffTotalGB`

```sql
COALESCE(c.total_gb_override,
  (SELECT
    CASE trf.traffic_strategy
      WHEN 'sum' THEN SUM(COALESCE(p.traffic, 0))
      ELSE (SELECT p_last.traffic FROM tariff_profiles tp_last
            JOIN profiles p_last ON p_last.id = tp_last.profile_id
            WHERE tp_last.tariff_id = cgr.tariff_id AND p_last.traffic IS NOT NULL
            ORDER BY tp_last.position DESC LIMIT 1)
    END * 1073741824
   FROM tariff_profiles tp
   JOIN profiles p ON p.id = tp.profile_id
   WHERE tp.tariff_id = cgr.tariff_id AND p.traffic IS NOT NULL),
  c.total_gb)
```

#### `sqlEffExpiry` (всегда overwrite)

```sql
CASE WHEN c.expiry_time_override IS NOT NULL THEN c.expiry_time_override
WHEN cgr.tariff_id IS NOT NULL AND c.tariff_started_at IS NOT NULL THEN
  c.tariff_started_at + (
    SELECT p_last.expiry_days
    FROM tariff_profiles tp_last
    JOIN profiles p_last ON p_last.id = tp_last.profile_id
    WHERE tp_last.tariff_id = cgr.tariff_id AND p_last.expiry_days IS NOT NULL
    ORDER BY tp_last.position DESC LIMIT 1
  ) * 86400000
ELSE c.expiry_time END
```

#### `sqlEffLimitIP` (всегда overwrite)

```sql
COALESCE(c.limit_ip_override,
  (SELECT p_last.limit_ip
   FROM tariff_profiles tp_last
   JOIN profiles p_last ON p_last.id = tp_last.profile_id
   WHERE tp_last.tariff_id = cgr.tariff_id AND p_last.limit_ip IS NOT NULL
   ORDER BY tp_last.position DESC LIMIT 1),
  c.limit_ip)
```

**JOIN в `newClientQuery`** — внешний `LEFT JOIN tariffs trf` **убирается**. Подзапросы `sqlEffTotalGB`, `sqlEffExpiry`, `sqlEffLimitIP` джойнят `tariffs` и `profiles` внутри себя через `cgr.tariff_id` (коррелированный подзапрос). `cgr` (`client_groups`) — единственный дополнительный JOIN снаружи:

```go
joins: []clientQueryJoin{
    {sql: "LEFT JOIN client_traffics ct ON ct.email = c.email"},
    {sql: "LEFT JOIN client_groups cgr ON cgr.name = c.group_name"},
},
```

Без `LEFT JOIN tariffs trf` — внешний `trf` больше нигде не используется.

**Важно:** подзапросы должны быть индексированы. Добавляем индекс:
```sql
CREATE INDEX IF NOT EXISTS idx_tariff_profiles_tariff
ON tariff_profiles(tariff_id, position);
```

### 2.3. Inbound-резолвер для xray config

В `client_link.go` метод `ListForInbound` сейчас вызывает `ToClientEffective(rec, tariff)`. Заменяется на `ToClientEffective(rec)` (без параметра). Inbound-ы в xray config не участвуют (они управляются через `client_inbounds`), поэтому inbound-цепочка не влияет на этот путь.

Inbound-ы тарифа применяются через существующий механизм `applyInboundList` только при:
- назначении тарифа группе
- добавлении клиента в группу
- return to tariff для поля inbounds

### 2.4. Остальные потребители

| Потребитель | Файл | Замена |
|-------------|------|--------|
| IP-limit job | `check_client_ip_job.go` | `loadClientLimits` — inline SQL обновить под подзапросы как в `sqlEffLimitIP` |
| Traffic refresh | `tariff.go:refreshTariffTraffic` | `EffectiveTotalGB(client)` / `EffectiveExpiryTime(client)` без параметра tariff |
| Traffic accounting | `inbound_traffic.go:resolveEffectiveTraffic` | inline SQL через подзапросы |
| `GET /:email` | `client.go:buildClientPayload` | Без изменений — отдаёт raw значения как сейчас |
| `GET /:email/effective` | `client.go` (новый хендлер) | Отдаёт `ToClientEffective(rec)` — effective-значения с учётом тарифа и override |
| OverrideField | `client_tariff.go` | `lookupTariffForGroup` → `resolveForClient` → `resolveChain`, затем берёт effective из chain |

---

## 3. Сервисный слой

### 3.1. `ProfileService` — `internal/web/service/profile.go`

```go
type ProfileService struct{}

type ProfileSummary struct {
    Id         int    `json:"id"`
    Name       string `json:"name"`
    Traffic    *int64 `json:"traffic"`
    ExpiryDays *int   `json:"expiryDays"`
    LimitIP    *int   `json:"limitIp"`
    InboundIds []int  `json:"inboundIds"`
    TariffCount int   `json:"tariffCount"`
    CreatedAt  int64  `json:"createdAt"`
    UpdatedAt  int64  `json:"updatedAt"`
}

func (s *ProfileService) List() ([]ProfileSummary, error)
func (s *ProfileService) Get(id int) (*ProfileSummary, error)
func (s *ProfileService) Create(name string, traffic *int64, expiryDays *int, limitIP *int, inboundIds []int) (*ProfileSummary, error)
func (s *ProfileService) Update(id int, name string, traffic *int64, expiryDays *int, limitIP *int, inboundIds []int) (*ProfileSummary, error)
func (s *ProfileService) Delete(id int) error // 409 если tariffCount > 0
```

Валидация: `name` не пустой. `traffic ≥ 0`, `expiryDays ≥ 0`, `limitIP ≥ 0` если не nil. `Delete` возвращает ошибку с перечнем имён тарифов, использующих профиль.

### 3.2. `TariffService` — `internal/web/service/tariff.go`

#### `TariffSummary` — новое

```go
type TariffSummary struct {
    Id              int               `json:"id"`
    Name            string            `json:"name"`
    TrafficStrategy string            `json:"trafficStrategy"`
    InboundStrategy string            `json:"inboundStrategy"`
    Enable          bool              `json:"enable"`
    Profiles        []TariffProfileItem `json:"profiles,omitempty"` // только в Get
    Resolved        *ResolvedFields   `json:"resolved,omitempty"`    // только в Get
    GroupCount      int               `json:"groupCount"`
    ClientCount     int               `json:"clientCount"`
    CreatedAt       int64             `json:"createdAt"`
    UpdatedAt       int64             `json:"updatedAt"`
}

type TariffProfileItem struct {
    Id       int    `json:"id"`
    Name     string `json:"name"`
    Position int    `json:"position"`
}

type ResolvedFields struct {
    Traffic    int64  `json:"traffic,omitempty"`    // bytes
    ExpiryDays int    `json:"expiryDays,omitempty"`
    LimitIP    int    `json:"limitIp,omitempty"`
    InboundIds []int  `json:"inboundIds,omitempty"`
}
```

#### Методы

```go
func (s *TariffService) List() ([]TariffSummary, error)
func (s *TariffService) Get(id int) (*TariffSummary, error)
func (s *TariffService) Create(name string, strategies TariffStrategies) (*TariffSummary, error)
func (s *TariffService) Update(id int, name string, strategies TariffStrategies) (*TariffSummary, error)
func (s *TariffService) SetProfiles(id int, profileIds []ProfilePosition) error
func (s *TariffService) Delete(id int) error
```

```go
type TariffStrategies struct {
    TrafficStrategy string
    InboundStrategy string
}

type ProfilePosition struct {
    Id       int `json:"id"`
    Position int `json:"position"`
}
```

#### `SetProfiles`

```go
func (s *TariffService) SetProfiles(id int, profileIds []ProfilePosition) error {
    return database.GetDB().Transaction(func(tx *gorm.DB) error {
        tx.Where("tariff_id = ?", id).Delete(&model.TariffProfile{})
        for _, pp := range profileIds {
            tx.Create(&model.TariffProfile{
                TariffID:  id,
                ProfileID: pp.Id,
                Position:  pp.Position,
            })
        }
        return nil
    })
}
```

**Known limitation:** между `Delete` и `Insert` внутри транзакции — окно, где у тарифа 0 профилей. Параллельный read-time резолв может на мгновение увидеть пустую цепочку и вернуть default-значения. SQLite с WAL-режимом и isolation по умолчанию: читатели видят снапшот до начала пишущей транзакции, так что на практике окна нет. При смене БД на PostgreSQL — проверить isolation level.
```

#### `refreshTariffTraffic` — обновляется

Использует `EffectiveTotalGB(client)` / `EffectiveExpiryTime(client)` без параметра tariff.

#### Удалённые методы

- `syncTariffInbounds` — inbound-ы теперь резолвятся read-time; привязка через `applyInboundList` происходит только в момент назначения тарифа группе / return to tariff.
- `RefreshTrafficForGroupReset` / `RefreshTrafficForGroup` — остаются, но вызывают новые signaless функции.

---

## 4. Контроллеры

### 4.1. `ProfileController` — `internal/web/controller/profile.go`

```go
type ProfileController struct {
    profileService service.ProfileService
}

func NewProfileController(g *gin.RouterGroup) *ProfileController
```

Роуты в `initRouter`:
```go
g.GET("/profiles", a.list)
g.GET("/profiles/:id", a.get)
g.POST("/profiles/create", a.create)
g.POST("/profiles/:id/update", a.update)
g.POST("/profiles/:id/delete", a.delete)
```

Контракты:

**POST `/profiles/create`** — body:
```json
{
  "name": "BASE",
  "traffic": 100,
  "expiryDays": 30,
  "limitIp": 3,
  "inboundIds": [1, 2]
}
```
`traffic`, `expiryDays`, `limitIp`, `inboundIds` — опциональны. Ответ — `ProfileSummary`.

**POST `/profiles/:id/update`** — body как create. Ответ — `ProfileSummary`.

**POST `/profiles/:id/delete`** — 409 с `{ "error": "profile used by tariffs: [Gold, Silver]" }` если tariffCount > 0.

**GET `/profiles`** — `[]ProfileSummary`.
**GET `/profiles/:id`** — `ProfileSummary`.

### 4.2. `GroupController` — обновление `internal/web/controller/group.go`

Добавить в `initRouter`:
```go
profileCtrl := NewProfileController(g)
_ = profileCtrl // инициализация регистрирует роуты
```

Обновить `tariffBody`:
```go
type tariffBody struct {
    Name            string `json:"name"`
    TrafficStrategy string `json:"trafficStrategy"`
    InboundStrategy string `json:"inboundStrategy"`
}
```

Новый роут:
```go
g.POST("/tariffs/:id/profiles", a.setTariffProfiles)
```

```go
func (a *GroupController) setTariffProfiles(c *gin.Context) {
    id, _ := strconv.Atoi(c.Param("id"))
    var body struct {
        ProfileIds []service.ProfilePosition `json:"profileIds"`
    }
    if err := c.ShouldBindJSON(&body); err != nil {
        jsonMsg(c, "error", err.Error())
        return
    }
    if err := a.tariffService.SetProfiles(id, body.ProfileIds); err != nil {
        jsonMsg(c, "error", err.Error())
        return
    }
    jsonMsg(c, "success", "profiles updated")
}
```

### 4.3. `ClientController` — `internal/web/controller/client.go`

Новый роут, существующий `GET /:email` **не трогаем**:

```go
g.GET("/:email/effective", a.getEffective)
```

```go
func (a *ClientController) getEffective(c *gin.Context) {
    email := c.Param("email")
    rec, err := a.clientService.GetByEmail(email)
    if err != nil {
        jsonMsg(c, "error", err.Error())
        return
    }
    effective := service.ToClientEffective(rec)
    jsonObj(c, effective)
}
```

Ответ — `model.Client` с effective-полями. Существующий `GET /:email` (`buildClientPayload`) — без изменений, возвращает raw как раньше.

### 4.4. Удалить из роутов

`POST /tariffs/:id/apply` — если ещё есть (в текущем коде был удалён).

---

## 5. Фронтенд

### 5.1. Zod-схемы

#### `frontend/src/schemas/profile.ts` (новый)

```ts
import { z } from 'zod'

export const ProfileSchema = z.object({
  id: z.number(),
  name: z.string(),
  traffic: z.number().int().nonnegative().nullable(),
  expiryDays: z.number().int().nonnegative().nullable(),
  limitIp: z.number().int().nonnegative().nullable(),
  inboundIds: z.array(z.number()),
  tariffCount: z.number().int().nonnegative(),
  createdAt: z.number().optional(),
  updatedAt: z.number().optional(),
})

export const ProfileFormSchema = z.object({
  name: z.string().min(1),
  traffic: z.number().int().nonnegative().nullable(),
  expiryDays: z.number().int().nonnegative().nullable(),
  limitIp: z.number().int().nonnegative().nullable(),
  inboundIds: z.array(z.number()),
})

export type Profile = z.infer<typeof ProfileSchema>
export type ProfileFormValues = z.infer<typeof ProfileFormSchema>
```

### 5.2. API query keys и хуки

Добавить в `frontend/src/api/queryKeys.ts`:
```ts
profiles: () => ['profiles'] as const,
```

API-хуки в `frontend/src/pages/groups/GroupsPage.tsx` (useQuery / useMutation через `@tanstack/react-query`).

### 5.3. Страница Groups — три вкладки

`frontend/src/pages/groups/GroupsPage.tsx`:

```tsx
<Tabs activeKey={activeTab} onChange={setActiveTab}>
  <Tabs.TabPane tab="Groups" key="groups">
    <GroupTable ... />
  </Tabs.TabPane>
  <Tabs.TabPane tab="Profiles" key="profiles">
    <ProfileTab ... />
  </Tabs.TabPane>
  <Tabs.TabPane tab="Tariffs" key="tariffs">
    <TariffTab ... />
  </Tabs.TabPane>
</Tabs>
```

### 5.4. `ProfileTab.tsx` (новый)

Таблица профилей: колонки Name, Traffic, Expiry, IPs, Inbounds (чипсы), Used by tariffs (число). Actions: Edit, Delete. Кнопка `+ New Profile`. Модалка create/edit с полями (name — required, остальные опциональны, `inboundIds` — Select multiple из multi-user inbounds).

### 5.5. `TariffTab` — обновлённая секция тарифов

Таблица: Name, Profiles (строка типа `BASE + PRO_ADDON`), Groups, Clients, Actions (Edit, Delete).

### 5.6. `TariffFormModal` — живой резолв

Модалка create/edit тарифа:

**Поля:**
- Name (Input)
- Profile chain — drag-and-drop список. Каждый элемент: `☰ ProfileName  —  100GB, 3 IP, vmess+vless`. Кнопка `+ Add Profile` — открывает Select с поиском по профилям.
- Порядок меняется drag-and-drop (использовать `@dnd-kit/core` + `@dnd-kit/sortable`, или Ant Design `Table` с `rowKey` и ручным перемещением через кнопки Up/Down).

**Стендовый мини-профиль (resolved preview):**
- Рендерится live при каждом изменении порядка, состава или стратегий.
- Поля:
  - Traffic: `{resolved} GB` + Select `sum | overwrite`
  - Expiry: `{resolved} days` (только overwrite, без стратегии)
  - IP limit: `{resolved}` (только overwrite, без стратегии)
  - Inbounds: `[{теги}]` + Select `overwrite | union`

**Логика live-резолва на фронте:**
Функция `resolveChainLocally(profiles, strategies)` — зеркало Go-резолвера. Вычисляет resolved preview без запроса на сервер. При save — отправляется полный список profileIds с позициями и стратегиями.

### 5.7. Клиентская форма — без изменений

`ClientFormModal.tsx`, `ManagedField.tsx`, `useTariffOverrides.ts` — не трогаем.

### 5.8. endpoints.ts

Добавить в `frontend/src/pages/api-docs/endpoints.ts`:
```ts
{
  method: 'GET',
  path: '/panel/api/clients/:email/effective',
  description: 'Get a client with resolved effective values (tariff chain + overrides)',
},
{
  method: 'GET',
  path: '/panel/api/clients/profiles',
  description: 'List all profiles',
},
{
  method: 'GET',
  path: '/panel/api/clients/profiles/:id',
  description: 'Get a single profile',
},
{
  method: 'POST',
  path: '/panel/api/clients/profiles/create',
  description: 'Create a profile',
  requestBody: { /* ProfileFormValues */ },
},
{
  method: 'POST',
  path: '/panel/api/clients/profiles/:id/update',
  description: 'Update a profile',
  requestBody: { /* ProfileFormValues */ },
},
{
  method: 'POST',
  path: '/panel/api/clients/profiles/:id/delete',
  description: 'Delete a profile (fails if used by tariffs)',
},
{
  method: 'POST',
  path: '/panel/api/clients/tariffs/:id/profiles',
  description: 'Set the ordered profile list for a tariff',
  requestBody: { profileIds: [{ id: number, position: number }] },
},
```

---

## 6. i18n

### Новые ключи (добавить во все 13 locale-файлов)

```json
{
  "menu.profiles": "Profiles",
  "pages.profiles.title": "Profiles",
  "pages.profiles.newProfile": "New Profile",
  "pages.profiles.name": "Name",
  "pages.profiles.traffic": "Traffic (GB)",
  "pages.profiles.expiryDays": "Expiry (days)",
  "pages.profiles.limitIp": "IP Limit",
  "pages.profiles.inboundIds": "Inbounds",
  "pages.profiles.usedByTariffs": "Used by tariffs",
  "pages.profiles.deleteBlocked": "Profile is used by tariffs: {names}",
  "pages.tariffs.profileChain": "Profile chain",
  "pages.tariffs.addProfile": "Add Profile",
  "pages.tariffs.resolvedPreview": "Resolved",
  "pages.tariffs.trafficStrategy": "Traffic strategy",
  "pages.tariffs.inboundStrategy": "Inbound strategy",
  "pages.tariffs.overwrite": "Overwrite",
  "pages.tariffs.sum": "Sum",
  "pages.tariffs.union": "Union"
}
```

### Удаляемые ключи (dead)

`pages.tariffs.tariffManagedBanner`, `pages.tariffs.tariffManagedFields`, `pages.tariffs.override`, `pages.tariffs.overrideSuccess`, `pages.tariffs.returnToTariffSuccess`, `pages.tariffs.applySuccess`, `pages.tariffs.applyPartial`, `pages.tariffs.applyNoChanges` — не используются, удалить.

---

## 7. OpenAPI / генерация

### `tools/openapigen/main.go`

Добавить в `StructAllow`:
```go
&model.Profile{},
&model.ProfileSummary{},
&model.TariffProfile{},
&model.EffectiveConfig{},
&model.ResolvedFields{},
```

Обновить `TariffSummary` (старая структура заменена).

### Регенерация

```bash
make gen
```

Убедиться что `frontend/src/generated/` и `frontend/public/openapi.json` обновлены без ошибок.

---

## 8. Тесты

### 8.1. Go-тесты — `internal/web/service/client_tariff_test.go`

| Тест | Что проверяет |
|------|--------------|
| `TestResolveChain_Overwrite` | Два профиля, overwrite → последний выигрывает по всем полям |
| `TestResolveChain_Sum` | traffic=100 + 500 = 600GB, expiry/IP всегда overwrite (последний) |
| `TestResolveChain_NullSkip` | Первый профиль traffic=nil, второй=100 → 100 (не 0) |
| `TestResolveChain_EmptyChain` | Пустой список профилей → все поля 0 |
| `TestResolveInbound_Overwrite` | Последний non-null заменяет |
| `TestResolveInbound_Union` | [1,2] + [2,3] → [1,2,3] |
| `TestResolveInbound_NullSkip` | [1,2], null, [3] → overwrite=[3], union=[1,2,3] |
| `TestResolveOverrides_OverrideWins` | client override перебивает chain |
| `TestResolveOverrides_NoTariff` | Клиент без группы → raw поля |
| `TestResolveOverrides_Expiry` | TariffStartedAt + chain_days → правильный timestamp |
| `TestResolveExpiry_NoStartedAt` | Нет TariffStartedAt → client.ExpiryTime |

Тесты используют `database.InitDB(filepath.Join(t.TempDir(), "x-ui.db"))` + `t.Cleanup`.

### 8.2. Фронтенд-тесты

Vitest-тесты для `resolveChainLocally` (зеркало Go-резолвера на TS).

---

## 9. Dead code к удалению

Из текущей ветки:
- `client_tariff.go`: `IsOverriddenTotalGB`, `IsOverriddenLimitIP`, `IsOverriddenExpiry`, `ApplyTariffToNewClient`
- `group.go`: `convertTariffSummaryToModel`
- `tariff.go`: `syncTariffInbounds` (заменяется)

Из `Tariff` struct: `TotalGB`, `ExpiryDays`, `LimitIP`, `InboundIds`.

---

## 10. Порядок реализации

1. **Модель:** `profile.go` + изменение `Tariff` в `model.go`
2. **DB:** `AutoMigrate` в `db.go`
3. **Резолвер:** переписать `client_tariff.go` под цепочку
4. **Сервисы:** `profile.go` + обновить `tariff.go`
5. **Контроллеры:** `profile.go` + обновить `group.go` + `client.go`
6. **SQL-выражения:** обновить `client_paging.go`, `check_client_ip_job.go`, `inbound_traffic.go`
7. **OpenAPI:** `openapigen/main.go` + `endpoints.ts`
8. **Фронтенд:** схемы, `ProfileTab`, `TariffFormModal` с живым резолвом, обновить `GroupsPage`
9. **i18n:** все 13 locale-файлов
10. **Тесты:** Go + фронтенд
11. `make gen && make verify`
