// E2E workflow(.github/workflows/e2e.yaml)がKVM/libvirt lab向けに4つのcontroller-manager/
// netboot-server imageをbuildするためのbake定義。docker-bake.release.hclと同じtarget構成だが、
// registryへpushせずlocal docker daemonへloadし、GitHub Actions cacheをjobをまたいで
// 再利用することでgo compileの重複を避ける。

variable "TAG" {
  default = "e2e"
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
  cache-from = [
    "type=gha,scope=e2e",
  ]
}

target "bootstrap-manager" {
  inherits = ["_common"]
  target   = "bootstrap-manager"
  tags     = ["bootstrap-controller:${TAG}"]
  output   = ["type=docker"]
}

target "control-plane-manager" {
  inherits = ["_common"]
  target   = "control-plane-manager"
  tags     = ["control-plane-controller:${TAG}"]
  output   = ["type=docker"]
}

target "infrastructure-manager" {
  inherits = ["_common"]
  target   = "infrastructure-manager"
  tags     = ["infrastructure-controller:${TAG}"]
  output   = ["type=docker"]
  # 4つのtargetでbuilderを共有しているのでGHA cache exportは代表1つだけでよい。
  cache-to = [
    "type=gha,mode=max,scope=e2e,ignore-error=true",
  ]
}

target "netboot-server" {
  inherits = ["_common"]
  target   = "netboot-server"
  tags     = ["netboot-server:${TAG}"]
  output   = ["type=docker"]
}
