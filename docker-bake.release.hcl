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
    "bootstrap-manager",
    "control-plane-manager",
    "infrastructure-manager",
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


target "bootstrap-manager" {
  inherits = [
    "_common",
  ]
  target = "bootstrap-manager"
  output = [
    "type=image,name=${REGISTRY}/${REPO}/bootstrap-manager,push-by-digest=true,name-canonical=true,push=true,compression=zstd,oci-mediatypes=true",
  ]
}

target "control-plane-manager" {
  inherits = [
    "_common",
  ]
  target = "control-plane-manager"
  output = [
    "type=image,name=${REGISTRY}/${REPO}/control-plane-manager,push-by-digest=true,name-canonical=true,push=true,compression=zstd,oci-mediatypes=true",
  ]
}

target "infrastructure-manager" {
  inherits = [
    "_common",
  ]
  target = "infrastructure-manager"
  # 4つのtargetでbuilderを共有しているのでGHA cache exportは代表1つだけでよい。
  cache-to = [
    "type=gha,mode=max,scope=build-${ARCH_KEY},ignore-error=true",
  ]
  output = [
    "type=image,name=${REGISTRY}/${REPO}/infrastructure-manager,push-by-digest=true,name-canonical=true,push=true,compression=zstd,oci-mediatypes=true",
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
