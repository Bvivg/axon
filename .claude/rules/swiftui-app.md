# SwiftUI (ui/app)

## Multiplatform
- Один target на iOS + macOS. Платформенные различия — через `#if os(macOS)` точечно (навигация, menu bar), не дублировать целые файлы.
- `NavigationSplitView` на macOS, `NavigationStack` на iOS — через platform-conditional обёртку, не размазывать `#if` по всем View.

## Архитектура
- `@Observable` view models на фичу (`QuoridorViewModel`), никакого бизнес-логики внутри `View` — только рендеринг + вызов методов view model.
- Networking (`Core/Networking`) — единая точка входа: `ConnectClient` (RPC) и `WebSocketClient` (realtime) как отдельные, независимо тестируемые типы.
- Модели данных (`Core/Models`) зеркалят сгенерированные из proto типы — не заводить параллельные ручные DTO.

## Игры
- Каждая игра — свой модуль в `Features/Games/<Game>/`: `<Game>BoardView.swift` + `<Game>ViewModel.swift` + доменные жесты/хелперы.
- Игровая доска — чистый рендеринг состояния из `GameState`, вся валидация ходов остаётся на бэкенде; клиент не дублирует правила игры (кроме UX-подсказок типа "куда можно сходить" — это хинт, не источник истины).

## Хранилище
- Токены — только Keychain, никогда UserDefaults.
- Никакого локального персистентного состояния игры сверх текущей сессии — источник истины сервер.
