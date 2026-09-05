package utils

type debugClusterAPIResource struct {
	Filename string
	Resource string
}

func debugClusterAPIResources() []debugClusterAPIResource {
	return []debugClusterAPIResource{
		{Filename: "clusters.yaml", Resource: "clusters.cluster.x-k8s.io"},
		{Filename: "clusterclasses.yaml", Resource: "clusterclasses.cluster.x-k8s.io"},
		{Filename: "machines.yaml", Resource: "machines.cluster.x-k8s.io"},
		{Filename: "machinesets.yaml", Resource: "machinesets.cluster.x-k8s.io"},
		{Filename: "machinedeployments.yaml", Resource: "machinedeployments.cluster.x-k8s.io"},
		{Filename: "machinepools.yaml", Resource: "machinepools.cluster.x-k8s.io"},
		{Filename: "machinehealthchecks.yaml", Resource: "machinehealthchecks.cluster.x-k8s.io"},
		{Filename: "kubeadmcontrolplanes.yaml", Resource: "kubeadmcontrolplanes.controlplane.cluster.x-k8s.io"},
		{Filename: "kubeadmconfigs.yaml", Resource: "kubeadmconfigs.bootstrap.cluster.x-k8s.io"},
		{Filename: "kubeadmconfigtemplates.yaml", Resource: "kubeadmconfigtemplates.bootstrap.cluster.x-k8s.io"},
		{Filename: "tartclusters.yaml", Resource: "tartclusters.infrastructure.cluster.x-k8s.io"},
		{Filename: "tarthosts.yaml", Resource: "tarthosts.infrastructure.cluster.x-k8s.io"},
		{Filename: "tarthostoperations.yaml", Resource: "tarthostoperations.infrastructure.cluster.x-k8s.io"},
		{Filename: "tartmachines.yaml", Resource: "tartmachines.infrastructure.cluster.x-k8s.io"},
		{Filename: "tartmachinetemplates.yaml", Resource: "tartmachinetemplates.infrastructure.cluster.x-k8s.io"},
	}
}
