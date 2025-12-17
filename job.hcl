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
        image = "jdxcode/mise:latest"
        args = [
          "exec",
          "go:github.com/lucasew/mise-ci/cmd/mise-ci",
          "worker"
        ]
      }

      env {
        MISE_CI_CALLBACK = "${NOMAD_META_callback_url}"
        MISE_CI_TOKEN    = "${NOMAD_META_token}"
      }

      resources {
        cpu    = 1000
        memory = 128
      }
    }
  }
}
