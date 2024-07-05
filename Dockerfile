FROM golang:1.22 AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -o /app

FROM alpine:3.20 as runner

COPY --from=builder /app /app

EXPOSE 8080

CMD ["/app"]
