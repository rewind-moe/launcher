FROM golang:1-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /bin/app

FROM alpine:3.12 AS app
COPY --from=builder /bin/app /bin/app
CMD ["/bin/app"]