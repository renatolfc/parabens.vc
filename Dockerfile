FROM rust:1.76-slim as builder
WORKDIR /app

# Cache dependencies
COPY Cargo.toml Cargo.lock ./
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    mkdir -p src && echo "fn main() {}" > src/main.rs && cargo build --release

COPY src ./src
COPY public ./public
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    --mount=type=cache,target=/app/target \
    cargo build --release

FROM debian:bookworm-slim
RUN useradd -m appuser
WORKDIR /app
COPY --from=builder /app/target/release/parabens-vc /usr/local/bin/parabens-vc
EXPOSE 8080
USER appuser
ENV RUST_LOG=info
CMD ["/usr/local/bin/parabens-vc"]
