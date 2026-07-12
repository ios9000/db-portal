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
#
# WU-035 (SPEC-035 mini-ADR 4): the dump playbook uploads the artifact to
# object storage (minio) with `mc`. Same reason it can't `apk add` at runtime
# (non-root uid 1001) — the client is baked in here at build time. Pinned to a
# specific mc release for reproducibility; the static amd64 binary matches the
# x86 VM runner.
FROM semaphoreui/semaphore:v2.17.39

ARG MC_RELEASE=RELEASE.2025-08-13T08-35-41Z

USER root
RUN apk add --no-cache postgresql16-client \
    && wget -q "https://dl.min.io/client/mc/release/linux-amd64/archive/mc.${MC_RELEASE}" -O /usr/local/bin/mc \
    && chmod 0755 /usr/local/bin/mc \
    && mkdir -p /artifacts \
    && chown 1001:0 /artifacts \
    && chmod 0775 /artifacts

USER 1001
