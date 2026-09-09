ARG GO_IMAGE=golang:1.26.5-alpine3.24@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2
ARG RUNTIME_IMAGE=alpine:3.22.5@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

FROM ${GO_IMAGE} AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
		-o /out/ ./cmd/api ./cmd/seed ./cmd/darkvoidctl \
	&& mv /out/api /out/darkvoid

FROM ${RUNTIME_IMAGE}

WORKDIR /app

# Keep the identity used by existing uploads volumes; see
# docs/uploads-volume-ownership-runbook.md. Only uploads needs to be writable.
# Alpine's BusyBox supplies wget for the Compose HTTP health check.
RUN apk add --no-cache ca-certificates tzdata \
	&& addgroup -S -g 101 darkvoid \
	&& adduser -S -u 100 -G darkvoid -h /app darkvoid \
	&& mkdir -p /app/uploads \
	&& chown darkvoid:darkvoid /app/uploads \
	&& chown root:root /app

COPY --from=builder /out/ /app/

USER darkvoid

EXPOSE 8080

ENTRYPOINT ["/app/darkvoid"]
