# Refactoring Learnings

## Task #3 - Extrair handshake inicial em `startWorker`
Data: 2025-12-27
Resultado: Complexidade reduzida, `startWorker` mais limpo
PR: TBD


## 2024-03-28 - Task #4
- **Task:** Extrair validação de auth em `WebSocketServer.HandleConnect`
- **Resultado:** Redução de complexidade de `HandleConnect`. Lógica de auth isolada em `validateWorkerAuth`.
- **Link do PR:** refactor/task-4
