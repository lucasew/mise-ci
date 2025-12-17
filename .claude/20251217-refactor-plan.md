# Plano de Refatoração: Abstração de Código Repetitivo

## Objetivo
Refatorar o codebase do mise-ci para reduzir duplicação de código e melhorar manutenibilidade, mantendo compatibilidade de protocolo entre worker e agent.

## Prioridades
- **Foco principal**: Manutenibilidade (código limpo e fácil de entender)
- **Áreas**: HTTP/SSE handlers, WebSocket/gRPC, Orquestração, Core/Listeners
- **Breaking changes internos**: Permitidos (desde que não quebrem comunicação worker-agent)
- **Novo**: Usar `mold` para abstrair templates

---

## Fase 1: Criar Pacotes de Utilidades

### 1.1. Pacote `internal/httputil`
**Criar**: `internal/httputil/response.go`

Funções helper para HTTP responses:
```go
// WriteJSON escreve resposta JSON
func WriteJSON(w http.ResponseWriter, status int, data interface{}) error

// WriteText escreve resposta de texto simples
func WriteText(w http.ResponseWriter, status int, message string)

// WriteError escreve erro formatado
func WriteError(w http.ResponseWriter, status int, format string, args ...interface{})

// WriteErrorMessage escreve erro simples
func WriteErrorMessage(w http.ResponseWriter, status int, message string)
```

**Arquivos afetados**:
- `internal/core/service.go` (linhas 51-52, 57-58, 63-64, 77-78, 96-97, 106-107)

### 1.2. Pacote `internal/sseutil`
**Criar**: `internal/sseutil/sse.go`

Abstrair boilerplate de SSE:
```go
// SetHeaders configura headers SSE padrão
func SetHeaders(w http.ResponseWriter)

// Flush faz flush do response writer se suportar
func Flush(w http.ResponseWriter)

// WriteEvent escreve um evento SSE com data em JSON
func WriteEvent(w http.ResponseWriter, data interface{}) error

// WriteEventString escreve evento SSE com data como string
func WriteEventString(w http.ResponseWriter, eventData string) error
```

**Arquivos afetados**:
- `internal/server/ui.go` (linhas 119-122, 163-166, 133-135, 152-154, 197-199, 127-130, 148-151, 190-195)

### 1.3. Pacote `internal/msgutil`
**Criar**: `internal/msgutil/builders.go`

Factory/builders para protobuf messages:
```go
// NewRunCommand cria comando Run
func NewRunCommand(id uint64, cmd string, args ...string) *pb.ServerMessage

// NewCopyToWorker cria comando de cópia para worker
func NewCopyToWorker(id uint64, source, dest string, data []byte) *pb.ServerMessage

// NewCopyFromWorker cria comando de cópia do worker
func NewCopyFromWorker(id uint64, source, dest string) *pb.ServerMessage

// NewCloseCommand cria comando Close
func NewCloseCommand(id uint64) *pb.ServerMessage
```

**Arquivos afetados**:
- `internal/core/service.go` (linhas 146-156, 192-201, 229-234, 343-348, 355-362)

---

## Fase 2: Abstrair Bidirectional Streams (CRÍTICO - 80% duplicação)

### 2.1. Criar Interface Genérica para Streams
**Criar**: `internal/stream/bidi.go`

```go
// MessageStream define interface para enviar/receber mensagens
type MessageStream[TSend, TRecv any] interface {
    Send(msg TSend) error
    Recv() (TRecv, error)
}

// BidiConfig configuração para handler bidirecional
type BidiConfig struct {
    Logger    *slog.Logger
    OnConnect func() error
    OnClose   func()
}

// HandleBidiStream gerencia comunicação bidirecional genérica
func HandleBidiStream[TSend, TRecv any](
    stream MessageStream[TSend, TRecv],
    sendCh <-chan TSend,
    recvCh chan<- TRecv,
    cfg BidiConfig,
) error
```

### 2.2. Refatorar WebSocket Handler
**Modificar**: `internal/server/websocket.go`

- Extrair lógica de sender/receiver para usar `HandleBidiStream`
- Manter tratamento específico de erros WebSocket
- Adaptar para interface `MessageStream`

**Linhas afetadas**: 98-172

### 2.3. Refatorar gRPC Handler
**Modificar**: `internal/server/grpc.go`

- Extrair lógica de sender/receiver para usar `HandleBidiStream`
- Manter tratamento específico de erros gRPC
- Adaptar para interface `MessageStream`

**Linhas afetadas**: 65-102

---

## Fase 3: Refatorar HTTP/SSE Handlers

### 3.1. Refatorar UIServer
**Modificar**: `internal/server/ui.go`

Usar helpers de `httputil` e `sseutil`:
```go
// HandleIndex - simplificar renderização
func (s *UIServer) HandleIndex(w http.ResponseWriter, r *http.Request) {
    runs := s.core.GetAllRuns()
    sort.Slice(runs, func(i, j int) bool {
        return runs[i].StartedAt.After(runs[j].StartedAt)
    })
    s.renderTemplate(w, "index.html", map[string]interface{}{"Runs": runs})
}

// renderTemplate - método helper consolidado
func (s *UIServer) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
    if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
        s.logger.Error("failed to render template", "template", name, "error", err)
        httputil.WriteError(w, http.StatusInternalServerError, "Internal Server Error")
    }
}

// HandleLogs - usar sseutil
func (s *UIServer) HandleLogs(w http.ResponseWriter, r *http.Request) {
    // ... validações ...
    sseutil.SetHeaders(w)

    // Enviar logs existentes
    info, _ := s.core.GetRunInfo(runID)
    for _, log := range info.Logs {
        sseutil.WriteEvent(w, map[string]interface{}{
            "timestamp": log.Timestamp,
            "stream":    log.Stream,
            "data":      log.Data,
        })
    }
    sseutil.Flush(w)

    // Stream novos logs...
}
```

**Linhas afetadas**: 75-78, 98-101, 119-122, 127-130, 133-135, 148-151, 152-154, 163-166, 190-195, 197-199

### 3.2. Refatorar Service Handlers
**Modificar**: `internal/core/service.go`

Usar `httputil` para responses:
```go
func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) {
    // ... lógica ...
    httputil.WriteText(w, http.StatusAccepted, "accepted")
    return
    // ou
    httputil.WriteText(w, http.StatusOK, "ignored")
}

func (s *Service) HandleTestDispatch(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        httputil.WriteError(w, http.StatusMethodNotAllowed, "only POST allowed")
        return
    }
    // ... lógica ...
    if err != nil {
        httputil.WriteError(w, http.StatusInternalServerError, "error: %v", err)
        return
    }
    httputil.WriteText(w, http.StatusOK, "dispatched: run_id=%s job_id=%s", runID, jobID)
}
```

**Linhas afetadas**: 51-52, 57-58, 63-64, 77-78, 96-97, 106-107

---

## Fase 4: Refatorar Orquestração (Remover Sleeps + Abstrair Comandos)

### 4.1. Criar Pipeline de Comandos
**Criar**: `internal/orchestration/pipeline.go`

```go
// Step representa um passo na orquestração
type Step struct {
    Name        string
    Description string
    Execute     func() error
}

// Pipeline executa uma série de steps com logging automático
type Pipeline struct {
    runID   string
    core    *core.Core
    logger  *slog.Logger
    steps   []Step
}

func NewPipeline(runID string, core *core.Core, logger *slog.Logger) *Pipeline

func (p *Pipeline) AddStep(name, description string, fn func() error) *Pipeline

func (p *Pipeline) Run() error
```

### 4.2. Refatorar TestOrchestrate
**Modificar**: `internal/core/service.go`

Substituir sleeps hardcoded + logs manuais por pipeline:
```go
func (s *Service) TestOrchestrate(ctx context.Context, run *Run) {
    <-run.ConnectedCh // Esperar worker

    pipeline := orchestration.NewPipeline(run.ID, s.Core, s.Logger)

    pipeline.
        AddStep("prepare", "Preparing test project files", func() error {
            return s.prepareTestProject(testDir)
        }).
        AddStep("copy-config", "Copying mise.toml to worker", func() error {
            return s.copyFileToWorker(run, 1, "mise.toml", "mise.toml", miseTomlData)
        }).
        AddStep("trust", "Trusting mise configuration", func() error {
            return s.runCommandSync(run, 2, "mise", "trust")
        }).
        AddStep("run-ci", "Starting CI task", func() error {
            return s.runCommandSync(run, 3, "mise", "run", "ci")
        }).
        AddStep("retrieve", "Retrieving output artifacts", func() error {
            return s.copyFileFromWorker(run, 4, "output.txt", outputPath)
        })

    if err := pipeline.Run(); err != nil {
        s.Core.UpdateStatus(run.ID, StatusFailure, nil)
        return
    }

    s.Core.UpdateStatus(run.ID, StatusSuccess, nil)
}
```

### 4.3. Criar Helpers de Comando
**Adicionar em**: `internal/core/service.go`

```go
// runCommandSync executa comando e espera completar (ou falhar)
func (s *Service) runCommandSync(run *Run, id uint64, cmd string, args ...string) error {
    s.Logger.Info("executing command", "cmd", cmd, "args", args)
    run.CommandCh <- msgutil.NewRunCommand(id, cmd, args...)

    if !s.waitForDone(run, cmd) {
        return fmt.Errorf("command failed: %s", cmd)
    }
    return nil
}

// copyFileToWorker envia arquivo para worker
func (s *Service) copyFileToWorker(run *Run, id uint64, source, dest string, data []byte) error {
    run.CommandCh <- msgutil.NewCopyToWorker(id, source, dest, data)
    if !s.waitForDone(run, fmt.Sprintf("copy %s", source)) {
        return fmt.Errorf("failed to copy file: %s", source)
    }
    return nil
}

// copyFileFromWorker recebe arquivo do worker
func (s *Service) copyFileFromWorker(run *Run, id uint64, source, dest string) error {
    run.CommandCh <- msgutil.NewCopyFromWorker(id, source, dest)
    if !s.receiveFile(run, dest, fmt.Sprintf("copy %s", source)) {
        return fmt.Errorf("failed to receive file: %s", source)
    }
    return nil
}
```

**Remover**: Todas as 9 ocorrências de `time.Sleep` (linhas 136, 162, 166, 173, 177, 184, 188, 207, 222)

**Linhas afetadas**: 110-226, 296-370

---

## Fase 5: Refatorar Core Listeners

### 5.1. Criar Generic Listener Manager
**Criar**: `internal/core/listeners.go`

```go
// ListenerManager gerencia lista de listeners com broadcast
type ListenerManager[T any] struct {
    mu        sync.RWMutex
    listeners []chan T
}

func NewListenerManager[T any]() *ListenerManager[T]

func (lm *ListenerManager[T]) Subscribe() chan T

func (lm *ListenerManager[T]) Unsubscribe(ch chan T)

func (lm *ListenerManager[T]) Broadcast(value T)
```

### 5.2. Refatorar Core para Usar ListenerManager
**Modificar**: `internal/core/core.go`

```go
type Core struct {
    runs           map[string]*Run
    runInfo        map[string]*RunInfo
    mu             sync.RWMutex
    logger         *slog.Logger
    jwtSecret      []byte
    logListeners   map[string]*ListenerManager[LogEntry]     // era: map[string][]chan LogEntry
    statusListener *ListenerManager[RunInfo]                  // era: []chan RunInfo
}

// Simplificar métodos:
func (c *Core) SubscribeLogs(runID string) chan LogEntry {
    c.mu.Lock()
    defer c.mu.Unlock()

    if _, ok := c.logListeners[runID]; !ok {
        c.logListeners[runID] = NewListenerManager[LogEntry]()
    }
    return c.logListeners[runID].Subscribe()
}

func (c *Core) UnsubscribeLogs(runID string, ch chan LogEntry) {
    c.mu.RLock()
    lm, ok := c.logListeners[runID]
    c.mu.RUnlock()

    if ok {
        lm.Unsubscribe(ch)
    }
}

func (c *Core) AddLog(runID string, stream string, data string) {
    c.mu.Lock()
    info, ok := c.runInfo[runID]
    if !ok {
        c.mu.Unlock()
        return
    }

    entry := LogEntry{
        Timestamp: time.Now(),
        Stream:    stream,
        Data:      data,
    }
    info.Logs = append(info.Logs, entry)

    lm, exists := c.logListeners[runID]
    c.mu.Unlock()

    if exists {
        lm.Broadcast(entry)
    }
}
```

**Remover**: Padrões duplicados de broadcast (linhas 92-96, 158-162, 189-194)
**Remover**: Funções duplicadas UnsubscribeLogs/UnsubscribeStatus (linhas 238-250, 261-272)

**Linhas afetadas**: 44-45, 62, 87-96, 137-162, 173-194, 215-272

---

## Fase 6: Templates com Mold (Novo)

### 6.1. Investigar e Integrar Mold
**Pesquisar**: Biblioteca `mold` para Go templates

### 6.2. Refatorar Template Loading
**Modificar**: `internal/server/ui.go`

Se mold for uma biblioteca de validação/transformação de structs, integrar no pipeline de dados de templates:
```go
// Validar e transformar dados antes de renderizar
func (s *UIServer) HandleIndex(w http.ResponseWriter, r *http.Request) {
    runs := s.core.GetAllRuns()
    sort.Slice(runs, func(i, j int) bool {
        return runs[i].StartedAt.After(runs[j].StartedAt)
    })

    data := struct {
        Runs []core.RunInfo `mold:"dive"`
    }{Runs: runs}

    // Validar/transformar com mold
    if err := s.moldTransformer.Struct(context.Background(), &data); err != nil {
        s.logger.Error("mold validation failed", "error", err)
    }

    s.renderTemplate(w, "index.html", data)
}
```

**Nota**: Implementação depende de confirmação sobre o que é "mold" no contexto desejado.

---

## Arquivos Críticos a Modificar

### Criar Novos
1. `internal/httputil/response.go`
2. `internal/sseutil/sse.go`
3. `internal/msgutil/builders.go`
4. `internal/stream/bidi.go`
5. `internal/orchestration/pipeline.go`
6. `internal/core/listeners.go`

### Modificar Existentes
1. `internal/server/ui.go` - Handlers HTTP/SSE
2. `internal/server/websocket.go` - Usar stream abstraction
3. `internal/server/grpc.go` - Usar stream abstraction (se existir)
4. `internal/core/service.go` - Orquestração + HTTP responses
5. `internal/core/core.go` - Listener management

---

## Garantias de Compatibilidade

### Não Quebrar
- ✅ Protocolo protobuf entre worker e agent
- ✅ Formato de mensagens WebSocket/gRPC
- ✅ Endpoints HTTP externos (/webhook, /test/dispatch, /ws)
- ✅ Formato de logs (timestamp, stream, data)
- ✅ Status transitions (scheduled → running → success/failure/error)

### Pode Mudar (Breaking Changes Internos OK)
- ✅ Assinaturas de métodos privados/internos
- ✅ Estrutura interna de packages
- ✅ Nomes de funções helper
- ✅ Organização de código

---

## Benefícios Esperados

### Manutenibilidade
- 📉 **-60%** de código duplicado em handlers SSE
- 📉 **-80%** de duplicação em bidirectional streams
- 📉 **-50%** de boilerplate em HTTP responses
- 📈 **+100%** de reutilização de código

### Legibilidade
- ✨ Intenção clara com pipelines de orquestração
- ✨ Menos detalhes de implementação em service.go
- ✨ Código mais idiomático Go (generics, interfaces)

### Confiabilidade
- 🔒 Remover sleeps hardcoded (delays arbitrários)
- 🔒 Centralizar tratamento de erros SSE/HTTP
- 🔒 Reduzir surface area para bugs de copy-paste

---

## Ordem de Implementação Recomendada

1. **Fase 1** - Criar pacotes utility (baixo risco, alto retorno)
2. **Fase 3** - Refatorar HTTP/SSE handlers (usa Fase 1)
3. **Fase 5** - Refatorar core listeners (isolado, moderado impacto)
4. **Fase 4** - Refatorar orquestração (usa Fase 1, moderado risco)
5. **Fase 2** - Abstrair bidirectional streams (alto impacto, requer atenção)
6. **Fase 6** - Integrar mold (experimental, baixo risco)

---

## Próximos Passos

1. ✅ Aprovar plano
2. ⚙️ Implementar Fase 1 (utilities)
3. ⚙️ Testar cada fase incrementalmente
4. ⚙️ Validar compatibilidade worker-agent após cada fase
5. ⚙️ Documentar novos patterns no código
