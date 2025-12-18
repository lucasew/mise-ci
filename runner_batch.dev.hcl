job "mise-ci-run" {
  type = "batch"

  priority = 0

  parameterized {
    payload       = "forbidden"
    meta_required = ["callback_url", "token"]
  }

  group "worker" {
    
    task "run" {
      driver = "docker"

      config {
        image = "jdxcode/mise:latest"
        network_mode = "host" # only for dev
        work_dir = "/local"
        args = [
          "exec",
          "go@latest",
          "--",
          "mise",
          "exec",
          "go:github.com/lucasew/mise-ci/cmd/mise-ci@main",
          "--",
          "mise-ci",
          "worker"
        ]
      }

      env {
        MISE_CI_CALLBACK = "${NOMAD_META_callback_url}"
        MISE_CI_TOKEN    = "${NOMAD_META_token}"
      }

      resources {
        cpu    = 1000
        memory = 1024 # TODO: package from binary
      }
    }
  }
}
