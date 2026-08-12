ARG POSTGRES_IMAGE=postgres:16.14-alpine@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777

FROM ${POSTGRES_IMAGE}

RUN apk add --no-cache bash ca-certificates curl jq restic tzdata \
	&& mkdir -p /state/cache \
	&& chown -R postgres:postgres /state

COPY --chown=postgres:postgres scripts/backup/postgres-restic.sh /usr/local/bin/postgres-restic.sh

USER postgres

ENTRYPOINT ["bash", "/usr/local/bin/postgres-restic.sh"]
