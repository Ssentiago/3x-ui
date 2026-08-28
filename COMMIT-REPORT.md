62 files, +33 682 / −27 093, net +6 589

Без i18n: 49 files, +6 270 / −772, net +5 498
i18n:      13 files, +27 412 / −26 321, net +1 091 (JSON reformatting churn, ~20 new keys)

═══ ГЛОБАЛЬНЫЙ ДИФФ ПО КАТЕГОРИЯМ ═══

Category                          Files  Added  Deleted    Net
FE components (new)                   8   1839        0  +1839
FE schemas (new)                      2     62        0    +62
FE test (new)                         1    168        0   +168
Go model (new)                        3    329        0   +329
Go service (new)                      3   1143        0  +1143
FE generated                          5   1323       17  +1306
FE clients                            7    205       65   +140
FE groups                             1     97      549   −452
FE infra                              3    135        3   +132
FE schemas                            2     28        9    +19
FE snapshots                          2    253        0   +253
Go models+migr                        3     61       29    +32
Go controllers                        2    290       12   +278
Go services                           4    228       73   +155
Go job                                1    102       14    +88
Go tooling                            2      7        1     +6
i18n                                 13  27412    26321  +1091
──────────────────────────────────────────────────────────────
TOTAL                                62  33682    27093  +6589


═══ ПОДРОБНЫЙ ДИФФ ПО ФАЙЛАМ ═══

=== Frontend: компоненты (новые, 8 файлов) ===
  +649    −0  frontend/src/pages/groups/GroupsTab.tsx       group table, CRUD modals, lazy sub-modals
  +404    −0  frontend/src/pages/groups/TariffFormModal.tsx  chain composer + live resolved preview
  +292    −0  frontend/src/pages/groups/ProfilesTab.tsx      profile table + form modal
  +234    −0  frontend/src/pages/groups/TariffsTab.tsx       tariff table + modal manager
   +79    −0  frontend/src/components/ManagedField.tsx       locked field overlay + resolvedDisplay
   +82    −0  frontend/src/hooks/useTariffOverrides.ts       makeLocal/returnToTariff hook
   +79    −0  frontend/src/lib/tariff/resolveChain.ts        TS chain resolver (Go mirror)
   +20    −0  frontend/src/pages/groups/TariffsTable.css     tariff table styles

=== Frontend: схемы (новые, 2 файла) ===
   +38    −0  frontend/src/schemas/tariff.ts                 Tariff, TariffFormSchema
   +24    −0  frontend/src/schemas/profile.ts                Profile, ProfileFormSchema

=== Frontend: тесты (новый, 1 файл) ===
  +168    −0  frontend/src/test/resolveChain.test.ts         13 mirror cases of Go TestResolveChain

=== Go: модель (новый, 1 файл) ===
   +41    −0  internal/database/model/profile.go             Profile, TariffProfile, TariffProfileItem, ResolvedFields

=== Go: сервисы (новые, 3 файла) ===
  +476    −0  internal/web/service/client_tariff_test.go     28 тестов: resolveChain (11), resolveOverrides (5), Effective* (6), SQL-vs-Go (5)
  +361    −0  internal/web/service/tariff.go                 TariffService: CRUD, SetProfiles, RefreshTrafficForGroup*, counts
  +306    −0  internal/web/service/client_tariff.go          resolveChain, resolveOverrides, Effective*, OverrideField, ReturnToTariff

=== Go: контроллер (новый, 1 файл) ===
  +104    −0  internal/web/controller/profile.go             GET/POST /profiles + /:id routes

=== Go: сервис (новый, 1 файл) ===
  +184    −0  internal/web/service/profile.go                ProfileService CRUD + tariff-usage guards

─── ИЗМЕНЁННЫЕ ───

=== Frontend: generated (5 файлов) ───
  +907   −17  frontend/public/openapi.json                   regenerated: 185 paths, 189 ops, 14 tags
   +61    −0  frontend/src/generated/examples.ts
  +231    −0  frontend/src/generated/schemas.ts
   +59    −0  frontend/src/generated/types.ts
   +65    −0  frontend/src/generated/zod.ts

=== Frontend: группы (1 файл) ───
   +97  −549  frontend/src/pages/groups/GroupsPage.tsx       1165→222 lines; split into three-tab container

=== Frontend: клиенты (7 файлов) ───
  +190   −46  frontend/src/pages/clients/ClientFormModal.tsx  ManagedField, override/return-to-tariff controls, tariff tag, resolvedDisplay
    +4    −7  frontend/src/pages/clients/ClientBulkAddModal.tsx     group prop: string[] → {name: string}[]
    +3    −4  frontend/src/pages/clients/ClientsPage.tsx            edit-hydration merge order fix
    +2    −2  frontend/src/pages/clients/BulkAddToGroupModal.tsx    group prop change
    +1    −2  frontend/src/pages/clients/BulkAttachInboundsModal.tsx group prop change
    +1    −2  frontend/src/pages/clients/BulkDetachInboundsModal.tsx group prop change
    +4    −2  frontend/src/pages/inbounds/clients/AddClientsToGroupModal.tsx group prop change

=== Frontend: инфраструктура (3 файла) ───
  +124    −0  frontend/src/pages/api-docs/endpoints.ts        Tariffs + Profiles sections; effective/override/returnToTariff routes
    +9    −3  frontend/src/layouts/AppSidebar.tsx             /groups nav active-state
    +2    −0  frontend/src/api/queryKeys.ts                   tariffs/profiles query keys

=== Frontend: схемы (2 файла) ───
   +24    −9  frontend/src/schemas/client.ts                  override columns, tariffName, *IsOverridden flags; GroupSummary tariff info
    +4    −0  frontend/src/schemas/primitives/protocol.ts     exported MULTI_USER_PROTOCOLS

=== Frontend: снапшоты (2 файла) ───
  +117    −0  frontend/src/test/__snapshots__/headers.test.ts.snap       additive: stale base snapshots
  +136    −0  frontend/src/test/__snapshots__/inbound-defaults.test.ts.snap additive: stale base snapshots

=== Go: модели + миграции (3 файла) ───
   +55   −29  internal/database/model/model.go               Tariff struct, ClientRecord override cols, ClientGroup.TariffID, ToClientEffective
    +3    −0  internal/database/db.go                        Tariff/Profile/TariffProfile in allModels()
    +3    −0  internal/database/migrate_data.go              Tariff/Profile/TariffProfile in migrationModels()

=== Go: контроллеры (2 файла) ───
  +241    −8  internal/web/controller/group.go               tariffId in create/rename; resetTariff; bulkAdd sync; list returns tariff info
   +49    −4  internal/web/controller/client.go              get/effective/:email; overrideField; returnToTariff routes

=== Go: сервисы (4 файла) ───
  +129   −43  internal/web/service/client_paging.go          sqlEffTotalGB/Expiry/LimitIP; loadGroupTariffs; effective filtering/sorting
   +73   −12  internal/web/service/client_groups.go           ListGroups with tariff info; CreateGroup(tariffId); AddToGroup overrides + started_at
   +24    −4  internal/web/service/inbound_traffic.go        resolveEffectiveTraffic for traffic counters
    +2   −14  internal/web/service/client_link.go            ListForInbound/ListForInboundBySubId emit ToClientEffective

=== Go: job (1 файл) ───
  +102   −14  internal/web/job/check_client_ip_job.go        resolve tariff limit_ip through tariff_profiles→profiles; resolveTariffLimitIPs()

=== Go: тулинг (2 файла) ───
    +6    −0  tools/openapigen/main.go                       StructAllow: Tariff, Profile, TariffProfile, ResolvedFields, TariffSummary
    +1    −1  internal/web/service/tgbot/tgbot_client.go     gofumpt (zero semantic change)

=== i18n: все 13 локалей ───
  +2110 −2026  ar-EG.json
  +2107 −2024  en-US.json
  +2108 −2024  es-ES.json
  +2109 −2025  fa-IR.json
  +2109 −2025  id-ID.json
  +2108 −2024  ja-JP.json
  +2108 −2024  pt-BR.json
  +2109 −2025  ru-RU.json
  +2109 −2025  tr-TR.json
  +2109 −2025  uk-UA.json
  +2109 −2025  vi-VN.json
  +2109 −2025  zh-CN.json
  +2108 −2024  zh-TW.json
  ───────────────────────
  +27412 −26321  net +1091  (JSON reformatting churn; ~20 new tariff/profile keys)
