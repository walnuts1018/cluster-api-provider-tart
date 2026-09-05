package driver

import (
	"context"

	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

type StaticAgentArtifactProvider struct {
	reference driverdomain.Artifact
}

func NewStaticAgentArtifactProvider(reference string) (StaticAgentArtifactProvider, error) {
	artifact, err := driverdomain.NewArtifact(reference)
	if err != nil {
		return StaticAgentArtifactProvider{}, err
	}
	return StaticAgentArtifactProvider{reference: artifact}, nil
}

func (provider StaticAgentArtifactProvider) VirtualMediaArtifact(
	context.Context,
	operationdomain.ID,
) (driverdomain.Artifact, error) {
	return provider.reference, nil
}
