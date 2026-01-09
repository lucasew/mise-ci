FROM ghcr.io/jdx/mise:latest@sha256:a55c391f7582f34c58bce1a85090cd526596402ba77fc32b06c49b8404ef9c14

COPY mise-ci /usr/local/bin/mise-ci

ENTRYPOINT ["/usr/local/bin/mise-ci"]
