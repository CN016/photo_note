FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/photo-nini-api ./cmd/api

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app
COPY --from=build /out/photo-nini-api /usr/local/bin/photo-nini-api
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/photo-nini-api"]
