# syntax=docker/dockerfile:1.6
#
# wallets-finder-5mins backend — Go HTTP server
# 多階段 build：alpine builder → minimal runtime

FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/wf5m-server ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Taipei

WORKDIR /app
COPY --from=builder /out/wf5m-server /app/wf5m-server

RUN adduser -D -u 10001 wf5m
USER wf5m

ENTRYPOINT ["/app/wf5m-server"]
