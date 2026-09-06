variable "REGISTRY" {
  default = "ghcr.io"
}

variable "REPO" {
  default = "walnuts1018/cluster-api-provider-tart"
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
}

target "manager" {
  inherits = [
    "_common",
  ]
  target = "manager"
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
