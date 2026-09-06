variable "REGISTRY" {
  default = "ghcr.io"
}

variable "REPO" {
  default = "walnuts1018/cluster-api-provider-tart"
}

variable "RELEASE_TAG" {
  default = "dev"
}

variable "REVISION" {
  default = ""
}

variable "ARCH_KEY" {
  default = "linux-amd64"
}

variable "PLATFORM" {
  default = "linux/amd64"
}


group "default" {
  targets = [
    "manager",
    "netboot-server",
  ]
}


target "_common" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms = [
    PLATFORM,
  ]
  cache-from = [
    "type=gha,scope=build-${ARCH_KEY}",
  ]
  labels = {
    "org.opencontainers.image.source"   = "https://github.com/${REPO}"
    "org.opencontainers.image.revision" = REVISION
    "org.opencontainers.image.version"  = RELEASE_TAG
  }
}


target "manager" {
  inherits = [
    "_common",
  ]
  target = "manager"
  # manager / netboot-serverでbuilderを共有しているのでGHA cache exportは片方だけでよい。
  cache-to = [
    "type=gha,mode=max,scope=build-${ARCH_KEY},ignore-error=true",
  ]
  output = [
    "type=image,name=${REGISTRY}/${REPO}/manager,push-by-digest=true,name-canonical=true,push=true,compression=zstd,oci-mediatypes=true",
  ]
}

target "netboot-server" {
  inherits = [
    "_common",
  ]
  target = "netboot-server"
  output = [
    "type=image,name=${REGISTRY}/${REPO}/netboot-server,push-by-digest=true,name-canonical=true,push=true,compression=zstd,oci-mediatypes=true",
  ]
}
