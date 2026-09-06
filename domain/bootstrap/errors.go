package bootstrap

import "errors"

// これらのerrorはTalos machine configuration合成における意思決定・検証の結果であり、
// 実際のYAML/CEL解釈を持たないためdomain層に置く。adapter/talos/configbuilderが実際の判定でこれらを返し、
// usecase/controllerはerrors.Isで分類する。
var (
	ErrCompleteConfigurationEmpty            = errors.New("complete machine configuration is empty")
	ErrEffectiveConfigurationIncomplete      = errors.New("effective machine configuration is incomplete")
	ErrEffectiveConfigurationInvalid         = errors.New("effective machine configuration is invalid")
	ErrConfigurationPatchEmpty               = errors.New("machine configuration patch is empty")
	ErrInstallDiskUnavailable                = errors.New("no install disk is available")
	ErrInstallDiskAmbiguous                  = errors.New("install disk cannot be selected unambiguously")
	ErrInstallConfigurationInvalid           = errors.New("machine configuration install target is invalid")
	ErrMachineConfigurationContextIncomplete = errors.New("machine configuration generation context is incomplete")
	ErrConfigurationConflict                 = errors.New("machine configuration conflicts with provider-owned invariant")
)
