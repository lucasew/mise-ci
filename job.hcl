job "mise-ci-run" {
  type = "batch"

  parameterized {
    payload       = "forbidden"
    meta_required = ["callback_url", "token"]
    meta_optional = ["image"]
  }

  group "worker" {
    task "run" {
      driver = "docker"

      config {
        image = "${NOMAD_META_image}"
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
