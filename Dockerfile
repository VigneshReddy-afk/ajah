# ---- builder ----------------------------------------------------------------
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency manifests first for layer caching — source changes do not
# invalidate the go mod download layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /gateway \
    ./cmd/gateway

# ---- final image ------------------------------------------------------------
# distroless/static has no shell, no package manager, no libc — minimal attack
# surface. The binary is statically linked so no libc is required.
FROM gcr.io/distroless/static-debian12:nonroot

EXPOSE 8080

# nonroot tag sets USER 65532 (nonroot) automatically.
COPY --from=builder /gateway /gateway

ENTRYPOINT ["/gateway"]
