FROM ghcr.io/jdx/mise:latest

COPY mise-ci /usr/local/bin/mise-ci

ENTRYPOINT ["/usr/local/bin/mise-ci"]
