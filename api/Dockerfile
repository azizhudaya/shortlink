# Static binary in a distroless nonroot image: near-zero
# attack surface, ~25 MB resident.

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/shortlink-api ./cmd/shortlink-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/shortlink-api /shortlink-api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/shortlink-api"]
