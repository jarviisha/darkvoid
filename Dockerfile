ARG GO_IMAGE=golang:1.26.5-alpine3.24@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2
ARG RUNTIME_IMAGE=alpine:3.22.5@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

FROM ${GO_IMAGE} AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/darkvoid ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/darkvoidctl ./cmd/darkvoidctl

FROM ${RUNTIME_IMAGE}

WORKDIR /app

# The uid and gid are pinned rather than left to adduser -S, which allocates
# whatever system id happens to be free in the base image. A Docker named volume
# takes its ownership from the image that first populated it, so the uploads
# volume on every existing deployment is owned by these exact numbers; letting a
# base image bump shift them would make the app unable to write its own uploads.
# 100:101 are the values adduser -S allocated when the unprivileged user was
# introduced, so pinning them changes nothing for volumes already in use.
# docs/uploads-volume-ownership-runbook.md depends on them too.
RUN apk add --no-cache ca-certificates tzdata wget \
	&& addgroup -S -g 101 darkvoid \
	&& adduser -S -u 100 -G darkvoid -h /app darkvoid \
	&& mkdir -p /app/uploads \
	&& chown -R darkvoid:darkvoid /app

COPY --from=builder --chown=darkvoid:darkvoid /out/darkvoid /app/darkvoid
COPY --from=builder --chown=darkvoid:darkvoid /out/seed /app/seed
COPY --from=builder --chown=darkvoid:darkvoid /out/darkvoidctl /app/darkvoidctl

USER darkvoid

EXPOSE 8080

ENTRYPOINT ["/app/darkvoid"]
