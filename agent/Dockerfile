FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/axyra-agent ./cmd/agent

FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl

WORKDIR /app
COPY --from=builder /bin/axyra-agent /bin/axyra-agent

EXPOSE 8083

CMD ["/bin/axyra-agent", "-config", "/etc/axyra/agent.yaml"]
