# mise-ci

A minimalist CI system based on mise tasks.

## Structure

- `cmd/mise-ci`: Single binary (agent + worker)
- `internal/`: System logic

## Setup

1. Install dependencies:
   ```bash
   mise install
   ```

2. Generate protobuf code:
   ```bash
   mise run codegen:protobuf
   ```

3. Build:
   ```bash
   mise run build
   ```

## Configuration

The system is configured preferably via environment variables with `MISE_CI_` prefix.
The structure follows the configuration file format (nested with `_` separator).

Examples:

- `MISE_CI_SERVER_HTTP_ADDR`: Listen address (e.g., `:8080`)
- `MISE_CI_JWT_SECRET`: JWT Secret
- `MISE_CI_GITHUB_APP_ID`: GitHub App ID (Enables GitHub support)
- `MISE_CI_GITHUB_PRIVATE_KEY`: Path to private key
- `MISE_CI_GITHUB_WEBHOOK_SECRET`: Webhook secret
- `MISE_CI_NOMAD_ADDR`: Nomad address (fallback to `NOMAD_ADDR`)
- `MISE_CI_NOMAD_JOB_NAME`: Parameterized job name (Required to enable Runner)
- `MISE_CI_NOMAD_DEFAULT_IMAGE`: Default worker image

## Usage

1. Start the agent (Server):
   ```bash
   ./bin/mise-ci agent
   ```

2. The worker is started automatically by Nomad, but can be run manually:
   ```bash
   ./bin/mise-ci worker --callback "http://localhost:8080" --token "..."
   ```

## Development

- `mise run ci`: Run lint and tests
