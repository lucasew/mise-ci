job "mise-ci" {
  type = "service"

  group "server" {
    count = 1
    network {
      port "www" {
        to = 8080
      }
    }

    service {
      provider = "nomad"
      port     = "www"
      tags = [
          "traefik.enable=true",
         	"traefik.http.middlewares.redirect-to-https.redirectscheme.scheme=https",
         	"traefik.http.routers.ci-http.entryPoints=http",
         	"traefik.http.routers.ci-http.middlewares=redirect-to-https",
         	"traefik.http.routers.ci-http.rule=Host(`ci.app.lew.tec.br`)",
         	"traefik.http.routers.ci-https.entryPoints=https",
         	"traefik.http.routers.ci-https.tls=true",
         	"traefik.http.routers.ci-https.rule=Host(`ci.app.lew.tec.br`)",
         	"traefik.http.routers.ci-https.tls.certresolver=letsencrypt"
        ]
    }

    task "agent" {
      driver = "docker"
      config {
        image = "jdxcode/mise:latest"
        ports = ["www"]

        # Using the same approach as the batch job:
        # using mise to install go and run the binary directly from the repo
        args = [
          "exec",
          "go@latest",
          "--",
          "mise",
          "exec",
          "go:github.com/lucasew/mise-ci/cmd/mise-ci@main",
          "--",
          "mise-ci",
          "agent"
        ]
      }
      env {
        MISE_CI_SERVER_HTTP_ADDR = "0.0.0.0:${NOMAD_PORT_http}"
        MISE_CI_STORAGE_DATA_DIR = "/alloc/data"
        MISE_CI_SERVER_PUBLIC_URL = "https://ci.app.lew.tec.br"
        MISE_CI_NOMAD_JOB_NAME = "mise-ci-run"

      }
      # Secrets and configuration from Nomad variables
      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/mise-ci" }}
MISE_CI_GITHUB_APP_ID         = "{{ .github_app_id }}"

{{ end }}

MISE_CI_NOMAD_ADDR            = "http://{{ env "attr.unique.network.ip-address" }}:4646"
MISE_CI_GITHUB_PRIVATE_KEY    = "/secrets/github-app.pem"

{{ with nomadVar "nomad/jobs/mise-ci/agent"}}
MISE_CI_JWT_SECRET            = "{{ .jwt_secret }}"
MISE_CI_AUTH_ADMIN_USERNAME   = "{{ .admin_username }}"
MISE_CI_AUTH_ADMIN_PASSWORD   = "{{ .admin_password }}"
MISE_CI_GITHUB_WEBHOOK_SECRET = "{{ .github_webhook_secret }}"
{{ end }}
EOH
        destination = "local/env"
        env         = true
      }
      template {
        data        = <<EOH
{{ with nomadVar "nomad/jobs/mise-ci"}}
{{ .github_private_key }}
{{ end }}
EOH
        destination = "secrets/github-app.pem"
      }
      resources {
        cpu    = 500
        memory = 1024 # TODO: package from binary
      }
    }
  }
}

