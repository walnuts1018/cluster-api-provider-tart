package controller

import (
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

func parseClusterID(value infrav1alpha1.ClusterID) (clusterdomain.ClusterID, error) {
	return clusterdomain.ParseClusterID(value.String())
}

func parseHostID(value infrav1alpha1.HostID) (hostdomain.HostID, error) {
	return hostdomain.ParseHostID(value.String())
}
