FROM ghcr.io/jdx/mise:latest@sha256:e1732a34debd36f1d5dfdcf9e357c56b639098094607fd5b43e22ed87bd47b5f

COPY mise-ci /usr/local/bin/mise-ci

ENTRYPOINT ["/usr/local/bin/mise-ci"]
