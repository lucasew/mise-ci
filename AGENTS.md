# AGENTS.md - Guia para Agentes de IA

Este documento serve como guia para agentes de IA (Claude Code, GitHub Copilot, etc.) trabalharem efetivamente no projeto mise-ci.

## Sobre o Projeto

mise-ci é um sistema de CI minimalista baseado em mise tasks. O projeto implementa uma arquitetura cliente-servidor onde:
- **Matriz** (servidor): recebe webhooks das forjas, gerencia runs e comanda workers
- **Worker** (efêmero): containers que executam os jobs, conectam via gRPC e streamam output

Para detalhes completos da arquitetura, consulte [SPEC.md](./SPEC.md).
Para instruções básicas de setup, consulte [README.md](./README.md).

## Ambiente de Desenvolvimento com Mise

**IMPORTANTE**: TODO o ambiente de desenvolvimento é gerenciado pelo mise. Isso inclui:
- Ferramentas e suas versões (Go, protoc, golangci-lint, sqlc, etc.)
- Tasks de desenvolvimento (build, codegen, lint, test)
- Workflow de desenvolvimento

O arquivo `mise.toml` define todas as ferramentas, versões e tasks. Leia-o para entender o ambiente completo.

### Instalação do Mise

Se o mise não estiver instalado no sistema:

1. Verifique se mise está disponível: `which mise`
2. Se não estiver, instale usando o script oficial:
   ```bash
   curl -fsSL https://mise.jdx.dev/install.sh -o /tmp/mise-install.sh
   bash /tmp/mise-install.sh
   rm /tmp/mise-install.sh
   ```

Documentação oficial: https://mise.jdx.dev

### Tasks Disponíveis

As tasks estão definidas no `mise.toml`. Para listar todas as tasks:
```bash
mise tasks
```

Tasks principais:
- `codegen` - Gera código (protobuf)
- `build` - Compila o binário
- `ci` - Executa lint e testes

## Workflow de Setup Inicial

```bash
mise install          # Instala todas as ferramentas
mise run codegen      # Gera código protobuf
mise run build        # Compila o binário
```

## Estrutura do Código

```
mise-ci/
├── cmd/mise-ci/              # Binário único (agent + worker)
├── internal/
│   ├── artifacts/            # Gerenciamento de artefatos
│   ├── config/               # Configuração
│   ├── forge/                # Interface e implementações de forjas (GitHub)
│   ├── proto/                # Definições protobuf
│   ├── repository/           # Camada de dados (sqlite, postgres)
│   ├── runner/               # Interface e implementações de runners (Nomad)
│   └── server/               # Servidor gRPC + HTTP
```

## Arquitetura

**Resumo rápido:**
- **Matriz**: servidor central que recebe webhooks, gerencia runs, comanda workers via gRPC
- **Worker**: binário efêmero que conecta via gRPC, executa comandos e streama output
- **Comunicação**: gRPC bidirecional com autenticação JWT

Para fluxo completo de uma run e detalhes do protocolo, veja [SPEC.md](./SPEC.md).

## Geração de Código

O projeto usa geração de código em dois lugares:

1. **Protobuf**: Define o protocolo gRPC entre matriz e workers
   - Comando: `mise run codegen:protobuf`
   - Gera código em: `internal/proto/`

2. **SQLC**: Gera código Go type-safe a partir de queries SQL
   - Comando: `sqlc generate`
   - Suporta dois backends: SQLite (dev) e PostgreSQL (prod)
   - Código gerado em: `internal/repository/sqlite/` e `internal/repository/postgres/`

## Configuração

A aplicação segue princípios 12-factor app. Configuração via variáveis de ambiente com prefixo `MISE_CI_`.

Principais variáveis:
- `MISE_CI_SERVER_HTTP_ADDR` - Endereço do servidor HTTP
- `MISE_CI_JWT_SECRET` - Segredo para assinar tokens
- `MISE_CI_GITHUB_APP_ID` - GitHub App ID
- `MISE_CI_NOMAD_ADDR` - Endereço do Nomad
- `MISE_CI_DATABASE_DRIVER` - Driver de banco (`sqlite` ou `postgres`)

Ver [SPEC.md](./SPEC.md) para lista completa.

## Adicionando Novas Features

### Nova Forja (GitLab, Gitea, etc.)
Implementar interface `Forge` em `internal/forge/`:
- `ParseWebhook`: interpreta webhook da forja
- `CloneCredentials`: retorna credenciais temporárias
- `UpdateStatus`: atualiza commit status
- Métodos de upload (artifacts, releases)

### Novo Runner (Kubernetes, Docker, etc.)
Implementar interface `Runner` em `internal/runner/`:
- `Dispatch`: spawna job para uma run
- `Cancel`: cancela job em andamento

### Novo Storage
Implementar interface `Storage` em `internal/artifacts/`:
- `Save`: salva artifact
- `Get`: recupera artifact
- `List`: lista artifacts de uma run

### Iteração no Schema de Banco de Dados

**High-level workflow**:

1. Editar schemas SQL em ambos backends:
   - `internal/repository/sqlite/schema/`
   - `internal/repository/postgres/schema/`

2. Criar migrations para ambos backends

3. Atualizar queries SQL em ambos backends:
   - `internal/repository/sqlite/queries/`
   - `internal/repository/postgres/queries/`

4. Regenerar código Go: `sqlc generate`

5. Atualizar código que usa o repository

**Nota**: Investigue os arquivos existentes em cada backend para entender padrões e detalhes de implementação.

## ⚠️ Armadilhas Comuns

### NÃO rodar comandos diretamente fora do mise

**Problema**: Rodar `go build`, `protoc`, `golangci-lint` ou outros comandos diretamente pode usar versões incompatíveis instaladas no sistema.

**Solução**: SEMPRE usar `mise run <task>` para garantir versões corretas das ferramentas.

Se o mise não estiver instalado, instale-o primeiro (veja seção "Instalação do Mise" acima) antes de executar qualquer comando.

### Ordem de execução importa

Sempre rodar `mise run codegen` antes de `mise run build` após mudanças no protobuf.

### Múltiplos backends de banco

Ao mudar o schema, lembre-se de atualizar AMBOS os backends (sqlite e postgres). Eles têm schemas e queries separados.

## Arquivos Ignorados no Git

Estes paths estão no `.gitignore`:
- `bin/` - Binários compilados
- `config.yaml` - Configuração local
- `*.pem` - Chaves privadas
- `data/` - Dados locais
- `build/` - Artefatos de build

## Comandos de Desenvolvimento Comuns

```bash
# Rebuild após mudar proto
mise run codegen && mise run build

# Regenerar código do banco após mudar schema/queries
sqlc generate

# Rodar CI localmente
mise run ci

# Build rápido
mise run build
```

## Princípios do Projeto

- **Minimalismo**: Apenas o essencial, sem over-engineering
- **12-factor app**: Configuração via environment variables
- **Single binary**: Um único binário serve como agent e worker
- **Baseado em mise tasks**: Todo workflow usa mise
- **Type-safe**: Uso de geração de código (sqlc, protobuf) para garantir type-safety
