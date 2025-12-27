# Refactoring Tasks

## Task #1 - Extrair lógica de `PushEvent` em `GitHubForge.ParseWebhook`
Arquivo: `internal/forge/github/github.go`
Problema: Complexidade 35, `switch` case grande para eventos
Ação: Criar função `handlePushEvent(e *github.PushEvent) (*forge.WebhookEvent, error)` e substituir lógica das linhas 43-73

---

## Task #2 - Extrair lógica de clone e setup em `Service.Orchestrate`
Arquivo: `internal/core/service.go`
Problema: Complexidade 29, função longa com muitos passos de preparação
Ação: Extrair passos de git clone, fetch, checkout, mise trust e install (linhas 407-430) para função helper `cloneAndSetup(...)`

---

## Task #3 - Extrair handshake inicial em `startWorker`
Arquivo: `cmd/mise-ci/worker.go`
Problema: Complexidade 29, mistura setup de conexão com loop de mensagens
Ação: Extrair lógica de envio de info e troca de contexto (linhas 87-124) para função `performHandshake(conn *websocket.Conn) (map[string]string, error)`

---

## Task #4 - Extrair validação de auth em `WebSocketServer.HandleConnect`
Arquivo: `internal/server/websocket.go`
Problema: Complexidade 20, mistura validação de token com upgrade de conexão
Ação: Extrair validação de header e token (linhas 69-90) para função `validateWorkerAuth(r *http.Request) (string, error)`

---

## Task #5 - Extrair atualização de status do forge em `CleanupStuckRuns`
Arquivo: `internal/core/maintenance.go`
Problema: Lógica aninhada dentro de loop para parsing de URL e chamada de API externa
Ação: Extrair bloco de atualização (linhas 116-155) para função helper `updateForgeStatus(ctx context.Context, run *repository.RunMetadata) bool`
