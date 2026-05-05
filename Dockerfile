FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /trove ./cmd/trove

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /trove /usr/local/bin/trove
VOLUME /data
EXPOSE 9000
ENTRYPOINT ["trove"]
