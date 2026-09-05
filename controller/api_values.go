package controller

import (
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

func parseClusterID(value string) (clusterdomain.ClusterID, error) {
	return clusterdomain.ParseClusterID(value)
}

func parseHostID(value string) (hostdomain.HostID, error) {
	return hostdomain.ParseHostID(value)
}
