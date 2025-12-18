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

A configuração é definida via variável de ambiente ou arquivo de configuração.

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

A aplicação segue os princípios do **12-factor app**, priorizando configuração via variáveis de ambiente.

O prefixo padrão é `MISE_CI`. A hierarquia é mapeada substituindo `.` por `_`.

### Variáveis principais

| Variável | Descrição | Exemplo |
|----------|-----------|---------|
| `MISE_CI_SERVER_HTTP_ADDR` | Endereço do servidor HTTP | `:8080` |
| `MISE_CI_SERVER_PUBLIC_URL` | URL pública (para webhooks/status) | `https://ci.example.com` |
| `MISE_CI_JWT_SECRET` | Segredo para assinar tokens | `mysecret` |
| `MISE_CI_AUTH_ADMIN_USERNAME` | Usuário admin | `admin` |
| `MISE_CI_AUTH_ADMIN_PASSWORD` | Senha admin | `secret` |
| `MISE_CI_GITHUB_APP_ID` | GitHub App ID | `12345` |
| `MISE_CI_GITHUB_PRIVATE_KEY` | Chave privada do App | `-----BEGIN...` |
| `MISE_CI_GITHUB_WEBHOOK_SECRET` | Segredo do webhook | `webhook_secret` |
| `MISE_CI_NOMAD_ADDR` | Endereço do Nomad | `http://nomad:4646` |
| `MISE_CI_DATABASE_DRIVER` | Driver de banco (`sqlite` ou `postgres`) | `postgres` |
| `MISE_CI_DATABASE_DSN` | Connection string | `postgres://...` |

Também é possível usar um arquivo de configuração (ex: `config.yaml`), mas variáveis de ambiente têm precedência.

## Tasks de desenvolvimento

O projeto utiliza `mise` para automação de tarefas de desenvolvimento, como:

- `mise run generate`: Gera código do protobuf.
- `mise run build`: Compila os binários `matriz` e `worker`.
- `mise run ci`: Executa pipeline de verificação (lint, tests).
