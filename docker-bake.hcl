# Builds the ext-php-rs builder images, one multi-arch tag per PHP/variant.

variable "IMAGE_NAME" {
  default = "shyim/php-extension-builder-rust"
}

variable "REGISTRY" {
  default = "ghcr.io"
}

variable "GIT_SHA" {
  default = ""
}

php_versions = ["8.2", "8.3", "8.4", "8.5"]

suffixes = {
  cli        = "cli"
  zts        = "zts"
  cli-alpine = "cli-alpine"
  zts-alpine = "zts-alpine"
}

function "tags" {
  params = [php, suffix]
  result = concat(
    ["${REGISTRY}/${IMAGE_NAME}:${php}-${suffix}"],
    GIT_SHA != "" ? ["${REGISTRY}/${IMAGE_NAME}:${php}-${suffix}-${GIT_SHA}"] : []
  )
}

group "default" {
  targets = ["image"]
}

target "image" {
  name       = "image-${replace(php, ".", "-")}-${suffix_key}"
  dockerfile = "docker/Dockerfile"
  context    = "."
  matrix = {
    php        = php_versions
    suffix_key = keys(suffixes)
  }
  args = {
    PHP_VERSION = php
    BASE_SUFFIX = suffixes[suffix_key]
  }
  tags      = tags(php, suffix_key)
  platforms = ["linux/amd64", "linux/arm64"]
}
