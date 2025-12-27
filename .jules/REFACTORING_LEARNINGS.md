
# Refactoring Learnings

## 2025-12-26 - Task #4
- **Task:** Extrair validação de auth em `WebSocketServer.HandleConnect`
- **Resultado:** Redução de complexidade de `HandleConnect`. Lógica de auth isolada em `validateWorkerAuth`.
- **Link do PR:** refactor/task-4

## 2025-12-26
- Date: 2025-12-27
- Task: #5
- Result: Extracted updateForgeStatus helper, improving modularity and testability.
- PR: refactor/task-5