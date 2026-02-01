FROM golang:1.22-alpine as builder
WORKDIR /app

# Cache dependencies
COPY go.mod go.sum* ./
RUN go mod download

# Copy source and public files (embedded)
COPY main.go main_test.go ./
COPY public ./public
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o parabens-vc .

FROM alpine:latest
RUN adduser -D -u 1000 appuser
WORKDIR /app
COPY --from=builder /app/parabens-vc /usr/local/bin/parabens-vc
EXPOSE 8080
USER appuser
CMD ["/usr/local/bin/parabens-vc"]
