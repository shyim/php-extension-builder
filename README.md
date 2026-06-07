# Generate Pre-Packaged Binaries for PHP Extensions

`php-extension-builder` is a Rust CLI for building pre-packaged binary archives
for [PIE (PHP Installer for Extensions)](https://github.com/php/pie) extensions.

It builds one target per invocation and writes a PIE-named `.zip` file containing
the compiled extension `.so`.

## Install

```bash
cargo install --path .
```

Or run it directly from a checkout:

```bash
cargo run -- build --help
```

Prebuilt CLI binaries are published on GitHub Releases for:

- `x86_64-apple-darwin`
- `aarch64-apple-darwin`
- `x86_64-unknown-linux-gnu`
- `aarch64-unknown-linux-gnu`

Release publishing is handled by GoReleaser. Pushing a tag like `v0.1.0`
triggers the release workflow, builds the CLI binaries with `cargo zigbuild`,
uploads `.tar.gz` archives, and publishes `checksums.txt`.

In GitHub Actions, prefer installing one of those release archives instead of
building the CLI with Cargo in every extension workflow:

```yaml
- name: Install php-extension-builder
  uses: jaxxstorm/action-install-gh-release@v3.0.0
  with:
    repo: shyim/php-extension-builder
    tag: v0.1.0
    cache: enable
```

The action selects the matching Linux or macOS archive for the current runner
and puts `php-extension-builder` on `PATH`.

## Linux Builds

Linux builds run inside official PHP Docker images. The CLI mounts the current
directory into the container, installs build dependencies inside the ephemeral
container, then runs any `--before-phpize-command` hooks followed by `phpize`,
`./configure`, and `make`.

Build logs are streamed as commands run. The builder also prints progress
markers for dependency installation, before-phpize hooks, `phpize`,
`./configure`, `make`, and metadata collection.

```bash
php-extension-builder build \
  --package-version 1.2.3 \
  --php-version 8.3 \
  --libc glibc \
  --configure-flag '--enable-example-pie-extension'
```

For Alpine/musl builds:

```bash
php-extension-builder build \
  --package-version 1.2.3 \
  --php-version 8.3 \
  --libc musl
```

For ZTS builds:

```bash
php-extension-builder build \
  --package-version 1.2.3 \
  --php-version 8.3 \
  --zts
```

If your extension needs extra system libraries, pass distro-specific packages.
The CLI installs `--apt-package` values only in Debian-based images and
`--apk-package` values only in Alpine-based images:

```bash
php-extension-builder build \
  --package-version 1.2.3 \
  --php-version 8.3 \
  --libc glibc \
  --apt-package libzstd-dev

php-extension-builder build \
  --package-version 1.2.3 \
  --php-version 8.3 \
  --libc musl \
  --apk-package zstd-dev
```

The default images are selected from the official PHP image family:

| Target      | Default image pattern       |
|-------------|-----------------------------|
| glibc NTS   | `php:<version>-cli`         |
| glibc ZTS   | `php:<version>-zts`         |
| musl NTS    | `php:<version>-cli-alpine`  |
| musl ZTS    | `php:<version>-zts-alpine`  |

Use `--image` to override the default image when a project needs extra system
dependencies:

```bash
php-extension-builder build \
  --package-version 1.2.3 \
  --php-version 8.3 \
  --image ghcr.io/acme/php-extension-builder:8.3
```

## macOS Builds

Docker cannot produce macOS PHP extension binaries because Docker Desktop runs
Linux containers. macOS packages must be built natively on a macOS host or
GitHub Actions macOS runner.

```bash
php-extension-builder build \
  --target-os darwin \
  --package-version 1.2.3 \
  --php-version 8.3 \
  --php-config /opt/homebrew/bin/php-config
```

The macOS backend runs any `--before-phpize-command` hooks followed by
`phpize`, `./configure`, and `make` on the host and packages the resulting
`modules/<extension>.so`. If `--php-version` is supplied, the selected
`php-config` must report that same PHP major/minor version. The default build is
non-ZTS; pass `--zts` when building against a ZTS PHP, and the CLI will fail if
the selected PHP thread-safety mode does not match. Native command logs are
streamed as each command runs.

## Windows Builds

Windows PHP extensions are built as `.dll` files with PHP's Windows build
tooling, not with Docker, `phpize`, or the Unix `./configure` flow. For Windows
artifacts, use [`php/php-windows-builder`](https://github.com/php/php-windows-builder).

The Windows builder provides GitHub Actions for generating the correct
PHP/version/architecture/thread-safety matrix and building extension DLLs:

```yaml
jobs:
  windows-matrix:
    runs-on: ubuntu-latest
    outputs:
      matrix: ${{ steps.extension-matrix.outputs.matrix }}
    steps:
      - uses: actions/checkout@v6
      - name: Get Windows extension matrix
        id: extension-matrix
        uses: php/php-windows-builder/extension-matrix@v1
        with:
          php-version-list: '8.2, 8.3, 8.4'
          arch-list: 'x64'
          ts-list: 'nts, ts'

  build-windows:
    needs: windows-matrix
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix: ${{ fromJson(needs.windows-matrix.outputs.matrix) }}
    steps:
      - uses: actions/checkout@v6
      - name: Build Windows extension
        uses: php/php-windows-builder/extension@v1
        with:
          php-version: ${{ matrix.php-version }}
          arch: ${{ matrix.arch }}
          ts: ${{ matrix.ts }}
          # args: --enable-your-extension
          # libs: zlib
```

Use this Rust CLI for Linux and macOS packages, and use
`php/php-windows-builder` for Windows DLL artifacts. If you upload Windows
artifacts to GitHub releases, prefer the builder's own release action so the
Windows artifact format stays aligned with that project.

## Options

| Option | Description |
|--------|-------------|
| `--package-version <version>` | Required. Used in the generated PIE package filename. |
| `--php-version <major.minor>` | Required for Linux Docker builds. Optional for macOS, where it validates the selected `php-config`. |
| `--target-os <linux\|darwin>` | Build backend. Defaults to `linux`. |
| `--libc <glibc\|musl\|bsdlibc>` | Linux defaults to `glibc`; Darwin defaults to `bsdlibc`. |
| `--zts` | Request a ZTS PHP build. Linux uses a ZTS Docker image; macOS validates the selected PHP is ZTS. Without it, the selected PHP must be non-ZTS. |
| `--build-path <path>` | Extension source path containing `config.m4`, relative to the current directory. Defaults to `.`. |
| `--configure-flag <flag>` | Additional flag passed to `./configure`. Can be supplied multiple times. |
| `--before-phpize-command <command>` | Shell command run before `phpize`. Can be supplied multiple times; quote commands that include arguments. |
| `--apt-package <package>` | Extra Debian/Ubuntu package installed with `apt-get`. Can be supplied multiple times. Linux Docker builds only. |
| `--apk-package <package>` | Extra Alpine package installed with `apk`. Can be supplied multiple times. Linux Docker builds only. |
| `--out-dir <path>` | Directory for the generated `.zip`. Defaults to the current directory. |
| `--image <image>` | Optional Docker image override for Linux builds. |
| `--php-config <path>` | Optional `php-config` path for native macOS builds. |

## Package Names

Generated archives keep PIE's expected naming convention:

```text
php_<extension>-<release>_php<php-version>-<arch>-<os>-<libc><debug><zts>.zip
```

Examples:

```text
php_example-1.2.3_php8.3-x86_64-linux-glibc.zip
php_example-1.2.3_php8.3-arm64-linux-musl-zts.zip
php_example-1.2.3_php8.3-arm64-darwin-bsdlibc.zip
```

## GitHub Actions Example

This example builds Linux and macOS PIE packages with the released builder
binary, uploads every matrix artifact, and publishes a GitHub release with
checksums when the workflow runs for a tag. Keep Windows builds in a separate
job using `php/php-windows-builder` as shown above.

```yaml
name: Release PIE binaries

on:
  push:
    tags: ["*"]
  workflow_dispatch:
    inputs:
      package_version:
        description: Package version used in generated package names. Defaults to the ref name without a leading v.
        required: false
        type: string

permissions:
  contents: read

env:
  PHP_EXTENSION_BUILDER_VERSION: v0.1.0
  ARTIFACT_DIR: dist

jobs:
  build-linux:
    runs-on: ubuntu-latest
    name: "PHP ${{ matrix.php-version }} / Linux ${{ matrix.libc }} / ${{ matrix.thread-safety }}"
    strategy:
      fail-fast: false
      matrix:
        php-version: ["8.2", "8.3", "8.4", "8.5"]
        libc: [glibc, musl]
        thread-safety: [nts, zts]
    steps:
      - name: Checkout
        uses: actions/checkout@v6.0.3

      - name: Resolve package version
        env:
          INPUT_PACKAGE_VERSION: ${{ inputs.package_version }}
        run: |
          if [ -n "$INPUT_PACKAGE_VERSION" ]; then
            version="$INPUT_PACKAGE_VERSION"
          else
            version="${GITHUB_REF_NAME#v}"
          fi

          echo "PACKAGE_VERSION=$version" >> "$GITHUB_ENV"

      - name: Install php-extension-builder
        uses: jaxxstorm/action-install-gh-release@v3.0.0
        with:
          repo: shyim/php-extension-builder
          tag: ${{ env.PHP_EXTENSION_BUILDER_VERSION }}
          cache: enable

      - name: Build extension
        shell: bash
        run: |
          mkdir -p "$ARTIFACT_DIR"

          zts_args=()
          if [ "${{ matrix.thread-safety }}" = "zts" ]; then
            zts_args+=(--zts)
          fi

          extra_args=(
            # --before-phpize-command "composer install --no-dev"
            # --configure-flag "--enable-your-extension"
            # --apt-package libzstd-dev
            # --apk-package zstd-dev
          )

          php-extension-builder build \
            --package-version "$PACKAGE_VERSION" \
            --php-version "${{ matrix.php-version }}" \
            --libc "${{ matrix.libc }}" \
            --out-dir "$ARTIFACT_DIR" \
            "${zts_args[@]}" \
            "${extra_args[@]}"

      - name: Upload package
        uses: actions/upload-artifact@v7.0.1
        with:
          name: php-extension-php${{ matrix.php-version }}-linux-${{ matrix.libc }}-${{ matrix.thread-safety }}
          path: ${{ env.ARTIFACT_DIR }}/*.zip
          if-no-files-found: error
          retention-days: 7

  build-macos:
    runs-on: ${{ matrix.target.runner }}
    name: "PHP ${{ matrix.php-version }} / macOS ${{ matrix.target.arch }} / nts"
    strategy:
      fail-fast: false
      matrix:
        php-version: ["8.2", "8.3", "8.4", "8.5"]
        target:
          - runner: macos-15-intel
            arch: x86_64
          - runner: macos-15
            arch: arm64
    steps:
      - name: Checkout
        uses: actions/checkout@v6.0.3

      - name: Resolve package version
        env:
          INPUT_PACKAGE_VERSION: ${{ inputs.package_version }}
        run: |
          if [ -n "$INPUT_PACKAGE_VERSION" ]; then
            version="$INPUT_PACKAGE_VERSION"
          else
            version="${GITHUB_REF_NAME#v}"
          fi

          echo "PACKAGE_VERSION=$version" >> "$GITHUB_ENV"

      - name: Setup PHP
        uses: shivammathur/setup-php@2.37.1
        with:
          php-version: ${{ matrix.php-version }}
          coverage: none

      - name: Install php-extension-builder
        uses: jaxxstorm/action-install-gh-release@v3.0.0
        with:
          repo: shyim/php-extension-builder
          tag: ${{ env.PHP_EXTENSION_BUILDER_VERSION }}
          cache: enable

      - name: Build extension
        shell: bash
        run: |
          mkdir -p "$ARTIFACT_DIR"

          extra_args=(
            # --before-phpize-command "composer install --no-dev"
            # --configure-flag "--enable-your-extension"
          )

          php-extension-builder build \
            --target-os darwin \
            --package-version "$PACKAGE_VERSION" \
            --php-version "${{ matrix.php-version }}" \
            --php-config "$(command -v php-config)" \
            --out-dir "$ARTIFACT_DIR" \
            "${extra_args[@]}"

      - name: Upload package
        uses: actions/upload-artifact@v7.0.1
        with:
          name: php-extension-php${{ matrix.php-version }}-darwin-${{ matrix.target.arch }}-nts
          path: ${{ env.ARTIFACT_DIR }}/*.zip
          if-no-files-found: error
          retention-days: 7

  publish:
    runs-on: ubuntu-latest
    name: Publish GitHub release
    needs:
      - build-linux
      - build-macos
    if: startsWith(github.ref, 'refs/tags/')
    permissions:
      contents: write
    steps:
      - name: Download packages
        uses: actions/download-artifact@v8.0.1
        with:
          path: packages

      - name: Prepare release assets
        shell: bash
        run: |
          mkdir -p dist
          find packages -type f -name '*.zip' -exec cp '{}' dist/ \;

          mapfile -t packages < <(find dist -maxdepth 1 -type f -name '*.zip' -print | sort)
          if [ "${#packages[@]}" -eq 0 ]; then
            echo "No packages found" >&2
            exit 1
          fi

          sha256sum "${packages[@]}" > dist/checksums.txt
          ls -lah dist

      - name: Publish release
        uses: softprops/action-gh-release@v3.0.0
        with:
          files: |
            dist/*.zip
            dist/checksums.txt
          fail_on_unmatched_files: true
          generate_release_notes: true
```
