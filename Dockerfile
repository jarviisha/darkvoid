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

RUN apk add --no-cache ca-certificates tzdata wget \
	&& addgroup -S darkvoid \
	&& adduser -S -G darkvoid -h /app darkvoid \
	&& mkdir -p /app/uploads \
	&& chown -R darkvoid:darkvoid /app

COPY --from=builder --chown=darkvoid:darkvoid /out/darkvoid /app/darkvoid
COPY --from=builder --chown=darkvoid:darkvoid /out/seed /app/seed
COPY --from=builder --chown=darkvoid:darkvoid /out/darkvoidctl /app/darkvoidctl

USER darkvoid

EXPOSE 8080

ENTRYPOINT ["/app/darkvoid"]
