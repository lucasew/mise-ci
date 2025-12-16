# mise-ci

Sistema de CI minimalista baseado em mise tasks.

## Arquitetura

```
┌─────────────┐     webhook      ┌─────────────┐
│    Forja    │ ───────────────► │   Matriz    │
│  (GitHub)   │ ◄─────────────── │   (agent)   │
└─────────────┘   commit status  └──────┬──────┘
                  releases              │
                  artifacts             │ gRPC stream
                                        │
                  ┌─────────────────────┼─────────────────────┐
                  │ Nomad               ▼                     │
                  │            ┌─────────────┐                │
                  │            │   Worker    │                │
                  │            │ (container) │                │
                  │            └─────────────┘                │
                  └───────────────────────────────────────────┘
```

### Componentes

**Matriz**: servidor central que:
- Recebe webhooks das forjas
- Gerencia runs (spawna jobs, acompanha execução)
- Comanda workers via gRPC streaming
- Atualiza status na forja
- Faz upload de artifacts e releases

**Worker**: binário efêmero que:
- Conecta na matriz via gRPC com JWT
- Recebe comandos (copy, run)
- Executa e streama output
- Envia arquivos quando solicitado
- Morre quando a conexão fecha

## Protocolo

Comunicação via gRPC bidirecional. Worker conecta, matriz comanda.

Ver `proto/ci.proto` para definição completa.

### Fluxo de uma run

1. Matriz recebe webhook da forja
2. Matriz cria JWT para a run
3. Matriz dispara job no runner (Nomad) passando callback URL + JWT
4. Worker inicia, conecta na matriz com JWT
5. Worker envia RunnerInfo
6. Matriz envia Copy (clone do repo com credenciais temporárias)
7. Worker clona, streama output, envia Done
8. Matriz envia Run (mise trust)
9. Worker executa, streama output, envia Done
10. Matriz envia Run (mise install)
11. Worker executa, streama output, envia Done
12. Matriz envia Run (mise run ci)
13. Worker executa, streama output, envia Done
14. Matriz opcionalmente pede arquivos (Copy FROM_WORKER)
15. Matriz envia Close
16. Worker desconecta, job termina
17. Matriz atualiza status na forja, faz upload de artifacts se houver

## Estrutura do projeto

```
mise-ci/
├── cmd/
│   ├── matriz/          # entrypoint do servidor
│   │   └── main.go
│   └── worker/          # entrypoint do worker
│       └── main.go
├── internal/
│   ├── forge/           # interface e implementações de forja
│   │   ├── forge.go     # interface Forge
│   │   └── github/      # implementação GitHub
│   ├── runner/          # interface e implementações de runner
│   │   ├── runner.go    # interface Runner
│   │   └── nomad/       # implementação Nomad
│   ├── proto/           # código gerado do protobuf
│   ├── server/          # servidor gRPC + HTTP da matriz
│   └── config/          # configuração
├── proto/
│   └── ci.proto         # definição do protocolo
├── go.mod
└── mise.toml
```

## Interfaces

### Forge

```go
type Forge interface {
    // ParseWebhook interpreta payload do webhook
    // Retorna nil se não for evento relevante (push, PR)
    ParseWebhook(r *http.Request) (*WebhookEvent, error)
    
    // CloneCredentials retorna credenciais temporárias para clone
    CloneCredentials(ctx context.Context, repo string) (*Credentials, error)
    
    // UpdateStatus atualiza commit status
    UpdateStatus(ctx context.Context, repo, sha string, status Status) error
    
    // UploadArtifact faz upload de artifact da run
    UploadArtifact(ctx context.Context, repo string, runID string, name string, data io.Reader) error
    
    // UploadReleaseAsset faz upload de asset para release
    UploadReleaseAsset(ctx context.Context, repo, tag, name string, data io.Reader) error
}

type WebhookEvent struct {
    Type   EventType // Push, PullRequest
    Repo   string    // owner/repo
    Ref    string    // refs/heads/main, refs/pull/123/merge
    SHA    string    // commit sha
    Clone  string    // clone URL
}

type Status struct {
    State       State  // Pending, Success, Failure, Error
    Context     string // "mise-ci"
    Description string
    TargetURL   string // link para logs
}

type State int
const (
    StatePending State = iota
    StateSuccess
    StateFailure
    StateError
)
```

### Runner

```go
type Runner interface {
    // Dispatch spawna job para uma run
    // Retorna ID do job
    Dispatch(ctx context.Context, params RunParams) (string, error)
    
    // Cancel cancela job em andamento
    Cancel(ctx context.Context, jobID string) error
}

type RunParams struct {
    CallbackURL string // URL da matriz
    Token       string // JWT para autenticação
    Image       string // imagem do container (opcional, usa default)
}
```

## Decisões técnicas

### Autenticação worker → matriz

JWT assinado pela matriz, contém:
- `run_id`: identificador da run
- `exp`: expiração curta (1h)

Worker passa JWT no connect. Matriz valida e associa conexão à run.

### Credenciais de clone

Matriz gera credenciais temporárias via API da forja (GitHub App installation token, etc).
Credenciais são passadas no comando Copy e nunca persistidas no worker.

### Streaming de output

Worker envia Output messages conforme lê stdout/stderr.
Matriz pode persistir, fazer broadcast para UI, etc.

### Artifacts

Matriz pede arquivo via Copy com direction FROM_WORKER.
Worker lê arquivo e envia em FileChunks.
Matriz faz upload para storage da forja ou S3.

### Configuração

```yaml
# matriz.yaml
server:
  http_addr: ":8080"    # webhooks
  grpc_addr: ":9090"    # workers

jwt:
  secret: "..."         # ou path para chave

forge:
  type: github
  app_id: 12345
  private_key: /path/to/key.pem
  webhook_secret: "..."

runner:
  type: nomad
  addr: "http://nomad:4646"
  job_template: /path/to/job.hcl
  default_image: "ghcr.io/mise-ci/worker:latest"

storage:
  type: s3              # para artifacts
  bucket: mise-ci-artifacts
  # ...
```

### Job template Nomad

```hcl
job "mise-ci-run" {
  type = "batch"
  
  parameterized {
    payload       = "forbidden"
    meta_required = ["callback_url", "token"]
  }
  
  group "worker" {
    task "run" {
      driver = "docker"
      
      config {
        image = "${image}"
      }
      
      env {
        MISE_CI_CALLBACK = "${NOMAD_META_callback_url}"
        MISE_CI_TOKEN    = "${NOMAD_META_token}"
      }
      
      resources {
        cpu    = 1000
        memory = 2048
      }
    }
  }
}
```

## Tasks de desenvolvimento

O projeto usa mise para desenvolvimento. Tasks disponíveis:

```toml
[tasks.generate]
description = "Gera código do protobuf"
run = "protoc --go_out=. --go-grpc_out=. proto/ci.proto"

[tasks.build]
description = "Compila binários"
run = """
go build -o bin/matriz ./cmd/matriz
go build -o bin/worker ./cmd/worker
"""

[tasks.ci]
description = "Roda checks de CI"
depends = ["ci:lint", "ci:test"]

[tasks."ci:lint"]
run = "golangci-lint run"

[tasks."ci:test"]
run = "go test ./..."
```

## Ordem de implementação sugerida

1. **Proto + geração de código**
   - Colocar ci.proto em proto/
   - Configurar geração com protoc

2. **Worker básico**
   - Conectar via gRPC
   - Receber e executar Copy (clone via git)
   - Receber e executar Run (exec genérico)
   - Streaming de output

3. **Matriz básica**
   - Servidor gRPC para workers
   - Gerenciamento de runs em memória
   - Comandar worker através de uma run

4. **Interface Forge + GitHub**
   - Parsing de webhooks
   - Commit status
   - Clone credentials via GitHub App

5. **Interface Runner + Nomad**
   - Dispatch de parameterized job
   - Cancelamento

6. **Servidor HTTP da matriz**
   - Endpoint de webhook
   - Endpoint de callback info (worker busca config)

7. **Integração completa**
   - Webhook → Run → Worker → Status update

8. **Artifacts**
   - Copy FROM_WORKER
   - Upload para S3/forge

9. **Releases**
   - Upload de assets para releases da forja
