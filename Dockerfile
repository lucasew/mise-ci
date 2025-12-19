FROM ghcr.io/jdx/mise:latest@sha256:edb72604e40cfb62512b9c2a120be60575f615128fa6fbf268cf44b3ef9e59de

COPY mise-ci /usr/local/bin/mise-ci

ENTRYPOINT ["/usr/local/bin/mise-ci"]
