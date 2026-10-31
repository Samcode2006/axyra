FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/analytics ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl

WORKDIR /app
COPY --from=builder /bin/analytics /bin/analytics

EXPOSE 8082

CMD ["/bin/analytics"]
