// Package controlplaneはTartControlPlaneのquorum安全性、cluster secret bundle世代管理、
// etcd bootstrap、Kubernetes version収束の純粋なpolicyを扱う。bundleのmaterial生成とTalos
// operationはTalos machineryへ委譲し、このpackageはimmutable Secretの境界と世代遷移を検証する。
package controlplane
