FROM golang:1.22-alpine AS builder
WORKDIR /app

# Cache dependencies
COPY go.mod go.sum* ./
RUN go mod download

# Copy source and public files (embedded)
COPY *.go ./
COPY public ./public
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o parabens-vc .

FROM archlinux:base
RUN pacman -Syu --noconfirm --needed ca-certificates librsvg ttf-opensans noto-fonts-emoji \
    && pacman -Scc --noconfirm
RUN useradd -m -u 1000 appuser
WORKDIR /app
COPY --from=builder /app/parabens-vc /usr/local/bin/parabens-vc
EXPOSE 8080
USER appuser
CMD ["/usr/local/bin/parabens-vc"]
