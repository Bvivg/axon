# Before: Axon, 18 ноября 2026

115 PNG:108 основных,6 воспроизведённого skip-link дефекта,1 transient Gateway error. Матрица основных экранов со стенда https://axon.test: 390/768/1440 CSS px × light/dark, viewport height900, fullPage. Только существующие тестовые аккаунты. Снимки Sessions/Notifications пересняты после загрузки, разговор — без отправки новых сообщений.

[Инвентаризация](../../quality-inventory.md) · [Manifest + hashes](manifest.json) · [DOM/axe checks](checks.json) · [Docker images](stand.json)

Админка снята под новым тестовым admin после отдельного разрешения владельца. Активная сцена звонка и аппаратные сценарии не запускались. Учётные данные вне репозитория.

| Экран | Light390 | Light768 | Light1440 | Dark390 | Dark768 | Dark1440 |
|---|---|---|---|---|---|---|
| Вход | [PNG](light/390/login.png) | [PNG](light/768/login.png) | [PNG](light/1440/login.png) | [PNG](dark/390/login.png) | [PNG](dark/768/login.png) | [PNG](dark/1440/login.png) |
| Регистрация | [PNG](light/390/register.png) | [PNG](light/768/register.png) | [PNG](light/1440/register.png) | [PNG](dark/390/register.png) | [PNG](dark/768/register.png) | [PNG](dark/1440/register.png) |
| Восстановление пароля | [PNG](light/390/forgot-password.png) | [PNG](light/768/forgot-password.png) | [PNG](light/1440/forgot-password.png) | [PNG](dark/390/forgot-password.png) | [PNG](dark/768/forgot-password.png) | [PNG](dark/1440/forgot-password.png) |
| Чаты, пустое состояние | [PNG](light/390/chat.png) | [PNG](light/768/chat.png) | [PNG](light/1440/chat.png) | [PNG](dark/390/chat.png) | [PNG](dark/768/chat.png) | [PNG](dark/1440/chat.png) |
| Заполненный чат | [PNG](light/390/conversation.png) | [PNG](light/768/conversation.png) | [PNG](light/1440/conversation.png) | [PNG](dark/390/conversation.png) | [PNG](dark/768/conversation.png) | [PNG](dark/1440/conversation.png) |
| Контакты | [PNG](light/390/contacts.png) | [PNG](light/768/contacts.png) | [PNG](light/1440/contacts.png) | [PNG](dark/390/contacts.png) | [PNG](dark/768/contacts.png) | [PNG](dark/1440/contacts.png) |
| Звонки, история | [PNG](light/390/calls.png) | [PNG](light/768/calls.png) | [PNG](light/1440/calls.png) | [PNG](dark/390/calls.png) | [PNG](dark/768/calls.png) | [PNG](dark/1440/calls.png) |
| Медиа | [PNG](light/390/media.png) | [PNG](light/768/media.png) | [PNG](light/1440/media.png) | [PNG](dark/390/media.png) | [PNG](dark/768/media.png) | [PNG](dark/1440/media.png) |
| Профиль | [PNG](light/390/profile.png) | [PNG](light/768/profile.png) | [PNG](light/1440/profile.png) | [PNG](dark/390/profile.png) | [PNG](dark/768/profile.png) | [PNG](dark/1440/profile.png) |
| Темы | [PNG](light/390/appearance.png) | [PNG](light/768/appearance.png) | [PNG](light/1440/appearance.png) | [PNG](dark/390/appearance.png) | [PNG](dark/768/appearance.png) | [PNG](dark/1440/appearance.png) |
| Сессии | [PNG](light/390/sessions.png) | [PNG](light/768/sessions.png) | [PNG](light/1440/sessions.png) | [PNG](dark/390/sessions.png) | [PNG](dark/768/sessions.png) | [PNG](dark/1440/sessions.png) |
| Уведомления | [PNG](light/390/notifications.png) | [PNG](light/768/notifications.png) | [PNG](light/1440/notifications.png) | [PNG](dark/390/notifications.png) | [PNG](dark/768/notifications.png) | [PNG](dark/1440/notifications.png) |
| Каталог игр | [PNG](light/390/games.png) | [PNG](light/768/games.png) | [PNG](light/1440/games.png) | [PNG](dark/390/games.png) | [PNG](dark/768/games.png) | [PNG](dark/1440/games.png) |
| Лобби Tic-Tac-Toe | [PNG](light/390/tic-tac-toe.png) | [PNG](light/768/tic-tac-toe.png) | [PNG](light/1440/tic-tac-toe.png) | [PNG](dark/390/tic-tac-toe.png) | [PNG](dark/768/tic-tac-toe.png) | [PNG](dark/1440/tic-tac-toe.png) |
| Локальная игра | [PNG](light/390/practice.png) | [PNG](light/768/practice.png) | [PNG](light/1440/practice.png) | [PNG](dark/390/practice.png) | [PNG](dark/768/practice.png) | [PNG](dark/1440/practice.png) |
| Администрирование | [PNG](light/390/admin.png) | [PNG](light/768/admin.png) | [PNG](light/1440/admin.png) | [PNG](dark/390/admin.png) | [PNG](dark/768/admin.png) | [PNG](dark/1440/admin.png) |
| Карта сервисов | [PNG](light/390/service-map.png) | [PNG](light/768/service-map.png) | [PNG](light/1440/service-map.png) | [PNG](dark/390/service-map.png) | [PNG](dark/768/service-map.png) | [PNG](dark/1440/service-map.png) |
| Gateway, успешный снимок | [PNG](light/390/gateway.png) | [PNG](light/768/gateway.png) | [PNG](light/1440/gateway.png) | [PNG](dark/390/gateway.png) | [PNG](dark/768/gateway.png) | [PNG](dark/1440/gateway.png) |
| Skip to content: ошибка чата | [PNG](light/390/chat-skip-error.png) | [PNG](light/768/chat-skip-error.png) | [PNG](light/1440/chat-skip-error.png) | [PNG](dark/390/chat-skip-error.png) | [PNG](dark/768/chat-skip-error.png) | [PNG](dark/1440/chat-skip-error.png) |

[Transient Gateway503, light390](light/390/gateway-error.png). Основной Gateway/light390 переснят после восстановления.

Backend: [coverage](backend-coverage.json), [test/build results](backend-checks.json), [vet](backend-vet.json), [source size](backend-size.json), [Compose config](backend-compose-result.json).
