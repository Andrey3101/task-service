# Task-Service

├── go.mod
├── go.sum
├── main.go
├── config\
│   └── (файлы, например, config.go)
├── handlers\
│   └── (файлы, например, task_handler.go)
├── middleware\
│   └── (файлы, например, middleware.go)
├── queue\
│   └── (файлы, например, queue.go)
├── services\
│   └── (файлы, например, task_service.go, auth_service.go, google_calendar.go)
└── Dockerfile

Сервис управления задачами с автоматическим распределением нагрузки.

## Текущая схема
- **Технологии**: Go 1.21, Gin, GORM, Redis, RabbitMQ
- **Функционал**:
  - Создание задач (`/tasks/create`, `/tasks/create/predictive`) с автоматическим или ручным назначением
  - Получение задач (`/tasks`, `/tasks/my` для User)
  - Обновление и удаление задач (`/:id`) через RabbitMQ
  - Добавление комментариев (`/:id/comments`)
  - Dashboard для Admin (`/dashboard`) с виджетами: нагрузка сотрудников, количество задач, просроченные задачи
  - Отчеты одним кликом (`/report/quick`, `/reports`)
  - WebSocket для обновлений задач (`/ws/tasks`)
  - Ротация JWT_SECRET каждые 24 часа
- **Зависимости**:
  - PostgreSQL (`tasks_db`) с шардированием по `auth_shard_0` и `auth_shard_1`
  - Redis для кэширования и Pub/Sub
  - RabbitMQ для асинхронной обработки задач
- **Структура БД**:
  - `tasks`: `id (UUID PK), title (String), description (String), status (String), due_date (DateTime), created_by (String), created_at (DateTime), priority (Int)`
  - `task_assignees`: `task_id (UUID FK), user_id (String FK)`
  - `comments`: `id (UUID PK), task_id (UUID FK), user_id (String), text (String), created_at (DateTime)`
  - `reports`: `id (UUID PK), company_id (UUID), period (String), data (String), created_at (DateTime)`

## Запуск
1. Установите зависимости: `go mod download`
2. Настройте переменные окружения в `docker-compose.yml`
3. Запустите: `docker-compose up`

## Предложенные доработки
1. **Приоритет задач с весами**:
   - Добавить веса задач (e.g., `priority` от 1 до 10) и учитывать их при распределении нагрузки.
2. **История изменений задач**:
   - Создать таблицу `task_history` для отслеживания изменений статуса и исполнителей.
   - Пример: `history_id (UUID PK), task_id (UUID), field (String), old_value (String), new_value (String), changed_at (DateTime)`.
3. **Фильтры в Dashboard**:
   - Добавить фильтры по дате, статусу и сотруднику через query-параметры (e.g., `/dashboard?date=2025-02`).
4. **Массовое назначение задач**:
   - Реализовать эндпоинт `/tasks/bulk_assign` для назначения нескольких задач одновременно.