# Refactoring Learnings

## 2024-03-28 - Task #4
- **Task:** Extrair validação de auth em `WebSocketServer.HandleConnect`
- **Resultado:** Redução de complexidade de `HandleConnect`. Lógica de auth isolada em `validateWorkerAuth`.
- **Link do PR:** refactor/task-4
