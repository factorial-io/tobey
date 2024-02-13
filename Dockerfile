FROM golang:1.22

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /app

EXPOSE 8080

CMD ["/app"]
