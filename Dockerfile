# syntax=docker/dockerfile:1
# Imagem do FinPi para o k3s: binário Go estático (sem cgo) numa imagem distroless, sem shell.
# Compila no processador da máquina de build e gera para a arquitetura de destino (arm64/amd64),
# então não precisa de emulação.
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSAO=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSAO}" -o /out/finpi .

FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /out/finpi /finpi
ENV HOST=0.0.0.0 \
    PORT=8080 \
    DB_PATH=/dados/finpi.db \
    GOMEMLIMIT=40MiB \
    GOGC=50
USER 10002:10002
EXPOSE 8080
ENTRYPOINT ["/finpi"]
