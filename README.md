# mise-ci

Um sistema de CI minimalista baseado em mise tasks.

## Estrutura

- `cmd/matriz`: Servidor central (gRPC + HTTP)
- `cmd/worker`: Agente de execução
- `internal/`: Lógica do sistema

## Setup

1. Instale dependências:
   ```bash
   mise install
   ```

2. Gere o código protobuf (se necessário):
   ```bash
   mise run generate
   ```

3. Compile:
   ```bash
   mise run build
   ```

## Configuração

Crie um arquivo `config.yaml` baseado em `exemplo.yaml`.

## Uso

1. Inicie a matriz:
   ```bash
   ./bin/matriz config.yaml
   ```

2. Configure o Webhook no GitHub apontando para `http://<seu-ip>:8080/webhook`.
3. Configure o Job no Nomad usando `job.hcl`.

## Desenvolvimento

- `mise run ci`: Roda lint e tests
