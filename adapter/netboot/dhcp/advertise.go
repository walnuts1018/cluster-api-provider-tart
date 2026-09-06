package dhcp

import (
	"errors"
	"fmt"
	"net"
)

// ResolveAdvertiseIPはクライアントへ広告するサーバーIPを解決する。
// advertiseAddrが明示的に設定されていればそれを使い、そうでなければbindAddr/httpAddrやnetwork interfaceから推測する。
func ResolveAdvertiseIP(bindAddr, httpAddr, advertiseAddr string) (net.IP, error) {
	if ip := net.ParseIP(advertiseAddr); ip != nil && !ip.IsUnspecified() {
		return ip, nil
	}

	for _, addr := range []string{bindAddr, httpAddr} {
		if ip := ParseHostIP(addr); ip != nil && !ip.IsUnspecified() {
			return ip, nil
		}
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("detect advertise address: %w", err)
	}
	var loopback net.IP
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsUnspecified() {
			continue
		}
		if ip.IsLoopback() {
			if loopback == nil {
				loopback = ip
			}
			continue
		}
		return ip, nil
	}
	if loopback != nil {
		return loopback, nil
	}
	return nil, errors.New("failed to detect advertise address")
}

// DefaultAdvertiseHTTPBaseURLは、advertise-http-base-urlが明示設定されていない場合に、
// 広告用IPとHTTP bind addressのポートから既定のHTTP base URLを組み立てる。
// 単一NICのシンプルな構成であればこの自動検出で足りるため、site固有の設定なしでも
// clusterctl/cluster-api-operatorでのインストール直後にnetboot-serverが動作できる。
func DefaultAdvertiseHTTPBaseURL(dhcpBindAddress, httpBindAddress, advertiseAddress string) (string, error) {
	advertiseIP, err := ResolveAdvertiseIP(dhcpBindAddress, httpBindAddress, advertiseAddress)
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(httpBindAddress)
	if err != nil {
		return "", fmt.Errorf("invalid HTTP bind address %s: %w", httpBindAddress, err)
	}
	return fmt.Sprintf("http://%s:%s", advertiseIP.String(), port), nil
}

// ParseHostIPはhost[:port]形式またはhost単体の文字列からIPを取り出す。
func ParseHostIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(host)
}
