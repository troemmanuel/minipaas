# syntax=docker/dockerfile:1

# ---- build stage --------------------------------------------------------
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./worker

# ---- runtime stage --------------------------------------------------------
# distroless "nonroot" already runs as uid 65532 with no shell, keeping the
# image minimal and free of a package manager / shell to reduce CVE surface.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /
COPY --from=builder /out/worker /usr/local/bin/worker

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/worker"]
