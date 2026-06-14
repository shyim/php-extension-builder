# Pre-baked PHP + Rust image for building ext-php-rs extensions.
ARG PHP_VERSION=8.3
ARG BASE_SUFFIX=cli

FROM php:${PHP_VERSION}-${BASE_SUFFIX}

ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH

RUN set -eux; \
    if command -v apk >/dev/null 2>&1; then \
        apk add --no-cache \
            ${PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c} \
            ca-certificates curl clang clang-dev llvm-dev pkgconf; \
    else \
        apt-get update; \
        DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
            ca-certificates curl clang libclang-dev llvm-dev pkg-config; \
        rm -rf /var/lib/apt/lists/*; \
    fi; \
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs \
        | sh -s -- -y --no-modify-path --profile minimal --default-toolchain stable; \
    rustc --version; \
    cargo --version

ENV LIBCLANG_PATH=/usr/lib
