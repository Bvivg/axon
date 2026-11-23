# Axon TV — лента живых партий

## Контекст
Владелец хочет «мониторинг матча, как Brawl TV / Royale TV»: вертикальная лента, которую
листают как короткие видео, на каждом экране — идущая сейчас партия любой игры, ходы
приходят в реальном времени. Решение владельца (2026-11-22): «оба, но url в games/tv» —
лента живёт на `/games/tv` в web, админка показывает её же у себя на `/admin/games/tv`.

Что уже есть и переиспользуется:
- `GameService.ListLiveSessions` — до 30 идущих партий (read model, `Viewer.View` под вызывающего).
- Зрительский режим в `/ws/game`: `subscribe` на чужую партию = зритель, ходить нельзя
  (`SESSION_ERROR_SPECTATOR`), Durak проходит через `Viewer.View`.
- Только-для-чтения рендер досок — `Position` в `ui/web/src/components/games/replay.tsx`.

Чего не хватает: сокет на каждый слайд упирается в лимиты gateway (60 апгрейдов в минуту и
16 одновременных сокетов на пользователя, общие с чатом и presence) — быстрое листание их
выбьет. Поэтому лента получает один мультиплексный зрительский сокет.

Результат: `/games/tv` в web и `/admin/games/tv` в админке показывают живые партии всех игр,
листаются свайпом/колесом/клавишами, Durak не раскрывает руки.

## Подход

### Контракт (`core/shared/proto/axon/game/v1/game.proto`, additive)
- `SessionCommand.action` += `WatchSessions watch = 10`, `message WatchSessions { repeated string session_ids = 1; }`.
- Первая команда `watch` переводит сокет в режим ленты: только чтение, набор до 3 партий,
  повторная `watch` заменяет набор. Ответы — обычные `SessionEvent{session}`.

### Game (`core/services/game/internal/server/tv.go`)
- `Socket`: первая команда `watch` → `h.watchFeed(...)`. Свою партию инстанс читает из памяти
  (`owner.Get`), изменения приходят через `h.updates`. На чужую партию лента открывает к владельцу
  внутренний relay-сокет `subscribe`, как обычный зритель на чужом инстансе, и пересылает его
  кадры; при переезде партии слот переподключается.
- Зритель ленты на своём инстансе анонимный: `h.view(..., uuid.Nil, ...)`; через relay владелец
  рендерит кадр под самого зрителя, он не игрок → `Viewer.View` так же прячет обе руки Durak
  и legal moves; после конца партии раскрытие как в replay.
- Дедуп пушей по `(revision, spectators)`, учёт в `watchers` (лимит и метрика как у обычного зрителя).
- Любая команда кроме `watch` → `SESSION_ERROR_SPECTATOR`; лимитер команд как в обычном сокете;
  больше 3 id, пустой или битый id → `SESSION_ERROR_INVALID_COMMAND`.
- Unit: `server/tv_test.go` на стенде из `durak_test.go` (несколько партий, переключение набора,
  отказ ходить, скрытые руки Durak).
- E2E: `core/services/game/e2e/tv_test.go` через gateway (`make test-e2e-game`).

### Web (`ui/web`)
- `src/app/(app)/games/tv/page.tsx` — лента в оболочке приложения.
- `src/app/(embed)/embed/tv/page.tsx` + `layout.tsx` — та же лента без оболочки для админки.
- `src/lib/ws/use-tv-socket.ts` — один сокет, `watch` на видимый слайд и соседей (debounce).
- `src/lib/games/tv.ts` — порядок ленты: новые партии в конец, просмотренные не исчезают (vitest).
- `src/components/games/position.tsx` — `Position` из replay, общий для replay и ленты.
- `src/components/games/tv/*` — snap-лента на `ScrollArea`, слайд (игра, игроки и рейтинги,
  номер хода, кнопка открыть партию), итог и «Next game», пустое состояние.
- Навигация: пункт «Axon TV» в `GamesSidebar`.
- Безопасность: `frame-ancestors 'self'` и `X-Frame-Options: SAMEORIGIN` только для `/embed/*`
  (`src/proxy.ts`, `src/lib/security/csp.ts`, `next.config.ts`), остальное по-прежнему `'none'`.

### Admin (`ui/admin`, без файлов PR #63)
- `src/app/(console)/games/tv/page.tsx` — iframe `/embed/tv` на всю область консоли.
- `src/lib/navigation.ts` — пункт «Axon TV» в группе Play; `isActive` не подсвечивает «Games»
  на более точном пункте.
- `src/lib/security/csp.ts` — `frame-src 'self'`.

## Как проверяется
- `cd core && go test -race ./services/game/... ./services/gateway/...` — зелёные, включая `tv_test.go`.
- `make test-e2e-game` (или `game-e2e` на любом стенде с оверлеем `docker-compose.game-e2e.yml`) —
  `TestTVFeedStreamsGamesOfDifferentKindsThroughTheGateway` проходит через gateway.
- `cd ui/web && npm run typecheck && npm run lint && npm test`; то же в `ui/admin`.
- Playwright `ui/web/e2e/tv.spec.ts` (`make test-web-e2e`): три партии разных игр, ход игрока
  появляется в ленте, листание колесом/клавишами на десктопе и свайпом на 375 px, у Durak нет
  лиц карт, итог партии и «Next game», пустое состояние; админ-сценарий — только с `ADMIN_POSTGRES_DSN`.
- Живой просмотр на одноразовом стенде `axon-tv` (base + game-e2e оверлей, web/admin в runtime-сборке),
  затем `down -v`.

## Расхождения с замыслом
- Итог партии — карточка под доской, а не поверх: поверх она закрывала выигрышную линию.
- При равной ревизии кадр сокета приоритетнее снимка из списка: в списке нет счётчика зрителей.
- `buf breaking` из worktree не запускается (`.git` — файл-ссылка вне контейнера); изменение аддитивное.
- Сначала чужие партии читались из read model, а зритель учитывался в `watchers` своего инстанса.
  В CI два инстанса game, и игрок не видел зрителя ленты: счётчик живёт у владельца. Теперь
  чужие партии идут через relay к владельцу, заодно без чтения Postgres на каждое изменение.

## Критерий готовности
`/games/tv` и `/admin/games/tv` на Docker-стенде показывают несколько живых партий разных
игр с ходами в реальном времени, Durak не раскрывает руки, e2e Go и Playwright зелёные.
