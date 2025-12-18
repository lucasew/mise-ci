# mise-ci

Sistema de CI minimalista baseado em mise tasks.

## Arquitetura

```
┌─────────────┐     webhook      ┌─────────────┐
│    Forja    │ ───────────────► │   Matriz    │
│  (GitHub)   │ ◄─────────────── │   (server)  │
└─────────────┘   commit status  └──────┬──────┘
                  releases              │
                                        │ gRPC stream
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

Ver `internal/proto/ci.proto` para definição completa.

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
│   ├── artifacts/       # gerenciamento de artefatos
│   │   ├── artifacts.go
│   │   └── local.go     # implementação filesystem local
│   ├── config/          # configuração
│   │   └── config.go
│   ├── forge/           # interface e implementações de forja
│   │   ├── forge.go     # interface Forge
│   │   └── github/      # implementação GitHub
│   ├── proto/           # definição e código gerado do protobuf
│   │   └── ci.proto
│   ├── repository/      # camada de dados (sqlc)
│   │   ├── postgres/    # implementação PostgreSQL
│   │   └── sqlite/      # implementação SQLite
│   ├── runner/          # interface e implementações de runner
│   │   ├── runner.go    # interface Runner
│   │   └── nomad/       # implementação Nomad
│   ├── server/          # servidor gRPC + HTTP da matriz
├── go.mod
├── mise.toml
└── sqlc.yaml
```

## Persistência

O projeto utiliza `sqlc` para gerar código Go type-safe a partir de queries SQL.
Suporta dois drivers de banco de dados:

- **SQLite**: para desenvolvimento local e setups simples.
- **PostgreSQL**: para produção.

A configuração é definida em `config.yaml` sob a chave `database`.

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
```

### Storage

A interface de storage de artifacts é desacoplada da Forja. A implementação padrão é `LocalStorage`, que salva arquivos em `data/artifacts`.

```go
type Storage interface {
    Save(ctx context.Context, runID string, name string, data io.Reader) error
    Get(ctx context.Context, runID string, name string) (io.ReadCloser, error)
    List(ctx context.Context, runID string) ([]string, error)
}
```

## Configuração

A configuração é carregada de `config.yaml`:

```yaml
# config.yaml
server:
  http_addr: ":8080"
  public_url: "https://ci.example.com"

jwt:
  secret: "..."         # segredo para assinar tokens dos workers

auth:
  admin_username: "admin"
  admin_password: "..." # basic auth para endpoints administrativos

github:
  app_id: 12345
  private_key: |
    -----BEGIN RSA PRIVATE KEY-----
    ...
  webhook_secret: "..."

nomad:
  addr: "http://nomad:4646"
  job_name: "mise-ci-worker"
  default_image: "ghcr.io/mise-ci/worker:latest"

storage:
  data_dir: "./data/artifacts"

database:
  driver: "sqlite"      # ou "postgres"
  dsn: "./mise-ci.db"   # ou "postgres://user:pass@host:5432/db"
```

## Tasks de desenvolvimento

O projeto usa mise para desenvolvimento. Tasks principais:

```toml
[tasks.generate]
description = "Gera código do protobuf"
run = "protoc --go_out=. --go-grpc_out=. internal/proto/ci.proto"

[tasks.build]
description = "Compila binários"
run = """
go build -o bin/matriz ./cmd/matriz
go build -o bin/worker ./cmd/worker
"""

[tasks.ci]
description = "Roda checks de CI"
depends = ["ci:lint", "ci:test"]
```
