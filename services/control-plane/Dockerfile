FROM golang:bookworm AS builder

WORKDIR /app

ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/control-plane ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl

WORKDIR /app
COPY --from=builder /bin/control-plane /bin/control-plane

EXPOSE 8081

CMD ["/bin/control-plane"]
