# Folder Size

Инструмент для поиска, что «съело» свободное место на диске. Go-бэкенд асинхронно и
конкурентно обходит файловую систему, считает размеры каталогов и кеширует дерево в
памяти. Веб-интерфейс общается с бэкендом по WebSocket.

## Запуск

```bash
go build -o folder-size .
./folder-size                    # http://127.0.0.1:8080
./folder-size -addr 127.0.0.1:9000
```

## Возможности

- Конкурентный обход (`NumCPU * 4`), отмена скана через `context`
- Живой прогресс по WebSocket
- Кеш дерева — drill-down без повторного чтения диска
- Сортировка/фильтр на клиенте, мини-treemap долей
- Удаление с пересчётом размеров у предков (размер берётся из кеша до `RemoveAll`)
- «Показать в Finder/Explorer», быстрые пути, недавние сканы
- Клавиатура: ↑↓ · Enter · Backspace · ⌘/Ctrl+Backspace или Delete

## Протокол WebSocket

Клиент → сервер: `{type:"scan"|"cancel"|"open"|"delete"|"reveal", path?}`

Сервер → клиент: `init` · `progress` · `scanned` · `children` · `deleted` · `cancelled` · `error`

## Структура

| Файл | Назначение |
|------|-----------|
| `node.go` | Модель узла и DTO |
| `cache.go` | Кеш каталогов, LookupSize / Remove |
| `scanner.go` | Конкурентный обход с отменой |
| `server.go` | Типизированный WebSocket-хаб |
| `main.go` | HTTP + `go:embed` статика |
| `web/` | UI без зависимостей |
