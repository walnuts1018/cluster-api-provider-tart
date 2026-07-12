package registrycredential

import (
	"fmt"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

func Load(configPath string) (auth.CredentialFunc, error) {
	if configPath == "" {
		return nil, nil
	}
	store, err := credentials.NewStore(configPath, credentials.StoreOptions{})
	if err != nil {
		return nil, fmt.Errorf("load registry credential file: %w", err)
	}
	return credentials.Credential(store), nil
}
