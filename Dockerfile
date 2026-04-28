ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.22

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/darkvoid ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

FROM alpine:${ALPINE_VERSION}

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata wget \
	&& addgroup -S darkvoid \
	&& adduser -S -G darkvoid -h /app darkvoid \
	&& mkdir -p /app/uploads \
	&& chown -R darkvoid:darkvoid /app

COPY --from=builder --chown=darkvoid:darkvoid /out/darkvoid /app/darkvoid
COPY --from=builder --chown=darkvoid:darkvoid /out/seed /app/seed

USER darkvoid

EXPOSE 8080

ENTRYPOINT ["/app/darkvoid"]
