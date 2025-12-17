# mise-ci

Um sistema de CI minimalista baseado em mise tasks.

## Estrutura

- `cmd/mise-ci`: Binário único (agent + worker)
- `internal/`: Lógica do sistema

## Setup

1. Instale dependências:
   ```bash
   mise install
   ```

2. Gere o código protobuf:
   ```bash
   mise run codegen:protobuf
   ```

3. Compile:
   ```bash
   mise run build
   ```

## Configuração

O sistema é configurado preferencialmente via variáveis de ambiente com prefixo `MISE_CI_`.
A estrutura segue o formato do arquivo de configuração (aninhamento separado por `_`).

Exemplos:

- `MISE_CI_SERVER_HTTP_ADDR`: Endereço de listen (ex: `:8080`)
- `MISE_CI_JWT_SECRET`: Segredo JWT
- `MISE_CI_FORGE_TYPE`: Tipo da forja (`github`)
- `MISE_CI_FORGE_APP_ID`: App ID do GitHub
- `MISE_CI_FORGE_PRIVATE_KEY`: Caminho para chave privada
- `MISE_CI_FORGE_WEBHOOK_SECRET`: Segredo do webhook
- `MISE_CI_RUNNER_TYPE`: Tipo do runner (`nomad`)
- `MISE_CI_RUNNER_ADDR`: Endereço do Nomad
- `MISE_CI_RUNNER_JOB_NAME`: Nome do job parametrizado
- `MISE_CI_RUNNER_DEFAULT_IMAGE`: Imagem padrão do worker

## Uso

1. Inicie o agente (Matriz):
   ```bash
   ./bin/mise-ci agent
   ```

2. O worker é iniciado automaticamente pelo Nomad, mas pode ser rodado manualmente:
   ```bash
   ./bin/mise-ci worker --callback "http://localhost:8080" --token "..."
   ```

## Desenvolvimento

- `mise run ci`: Roda lint e tests
