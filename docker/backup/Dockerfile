ARG POSTGRES_VERSION=16-alpine

FROM postgres:${POSTGRES_VERSION}

RUN apk add --no-cache bash ca-certificates curl jq restic tzdata \
	&& mkdir -p /state/cache \
	&& chown -R postgres:postgres /state

COPY --chown=postgres:postgres scripts/backup/postgres-restic.sh /usr/local/bin/postgres-restic.sh

USER postgres

ENTRYPOINT ["bash", "/usr/local/bin/postgres-restic.sh"]
