ARG SOCKET_GEN_VERSION=main

FROM golang:alpine as builder
ARG SOCKET_GEN_VERSION
WORKDIR /build
COPY . .
RUN go mod download
RUN GOOS=linux CGO_ENABLED=0 go build -ldflags "-X main.buildVersion=${SOCKET_GEN_VERSION}" -o socket-gen ./cmd/socket-gen

FROM nginx:alpine-slim
WORKDIR /app
COPY --from=builder /build/socket-gen /app/socket-gen
COPY example/nginx/default.tmpl /app/default.tmpl
COPY example/nginx/99-socket-gen.sh /docker-entrypoint.d

