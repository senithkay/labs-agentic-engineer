FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/local-secret-manager-api .

FROM alpine:3.21
RUN addgroup -g 1000 appgroup && adduser -S -u 1000 -G appgroup appuser
WORKDIR /app
COPY --from=builder /app/bin/local-secret-manager-api .
USER appuser
EXPOSE 8082
ENTRYPOINT ["./local-secret-manager-api"]
