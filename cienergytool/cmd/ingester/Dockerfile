# cienergy-ingester — multi-stage build, distroless runtime, < 25 MB.
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo dev)" \
      -o /out/cienergy-ingester ./cmd/ingester

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cienergy-ingester /usr/local/bin/cienergy-ingester
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/cienergy-ingester"]

