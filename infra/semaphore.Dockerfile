# WU-034 (SPEC-034 mini-ADR 2): the dev Semaphore runner, with a real
# postgresql-client baked in so playbooks/dump.yml can run pg_dump.
#
# WHY a custom image and not a playbook `apk add` setup task: the Semaphore
# runner executes as uid 1001 (non-root); `apk add` needs root, so a runtime
# install fails. We add the client here, as root, at build time.
#
# WHY provision /artifacts here: a fresh named volume mounted at /artifacts
# inherits THIS mountpoint's ownership on first mount, so making it 1001:0
# 0775 in the image is what lets the non-root runner write dumps into the
# shared volume. (Docker only copies image ownership into an EMPTY volume.)
#
# Pinned to v2.17.39 for the same reason as compose.yaml (v2.18.x panics on
# BoltDB boot). postgresql16-client (16.14 in Alpine 3.21 main) dumps a
# postgres:16 target exactly; pg_restore --list reads the custom-format TOC.
FROM semaphoreui/semaphore:v2.17.39

USER root
RUN apk add --no-cache postgresql16-client \
    && mkdir -p /artifacts \
    && chown 1001:0 /artifacts \
    && chmod 0775 /artifacts

USER 1001
