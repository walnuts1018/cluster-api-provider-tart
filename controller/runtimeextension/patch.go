package runtimeextension

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

func planMachineUpdate(req *runtimehooksv1.CanUpdateMachineRequest) (runtimehooksv1.Patch, runtimehooksv1.Patch, runtimehooksv1.Patch, error) {
	desiredInfrastructure, err := decodeTartMachine(req.Desired.InfrastructureMachine)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	if _, err := talos.InstallerImage(desiredInfrastructure.Spec.Image.Version, desiredInfrastructure.Spec.Image.SchematicID); err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	currentMachine, err := marshalMap(req.Current.Machine.Spec)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	desiredMachine, err := marshalMap(req.Desired.Machine.Spec)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	// Kubernetes version変更はTartControlPlaneが所有するcluster-wide upgradeで実行済みであり、
	// Machine単位ではdesired versionの伝播だけを許可する。UpdateMachineでは自Nodeのobserved versionが
	// desired versionへ収束したことだけを確認する。cluster、bootstrap、infrastructure、ProviderIDなどの
	// 差分は引き続きpatch経由で変更できないようにする。
	machinePatch, err := planSpecPatch(currentMachine, desiredMachine, machineUpdatableSpecPaths, "/spec")
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	infrastructurePatch, err := planRawObjectPatch(req.Current.InfrastructureMachine, req.Desired.InfrastructureMachine, infrav1alpha1.GroupVersion.String(), tartMachineKind, []string{imageField, versionField}, []string{imageField, "schematicID"})
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	bootstrapPatch, err := planBootstrapConfigPatch(req.Current.BootstrapConfig, req.Desired.BootstrapConfig)
	return machinePatch, infrastructurePatch, bootstrapPatch, err
}

func planMachineSetUpdate(req *runtimehooksv1.CanUpdateMachineSetRequest) (runtimehooksv1.Patch, runtimehooksv1.Patch, runtimehooksv1.Patch, error) {
	if err := validateTemplateImage(req.Desired.InfrastructureMachineTemplate); err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	currentMachine, err := marshalMap(req.Current.MachineSet.Spec.Template.Spec)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	desiredMachine, err := marshalMap(req.Desired.MachineSet.Spec.Template.Spec)
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	// MachineSetのtemplateでも、Kubernetes version upgradeそのものはTartControlPlaneが所有する。
	// このExtensionはdesired versionの伝播とTalos OS image変更だけを許可する。
	machinePatch, err := planSpecPatch(currentMachine, desiredMachine, machineUpdatableSpecPaths, "/spec/template/spec")
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	infrastructurePatch, err := planRawTemplatePatch(req.Current.InfrastructureMachineTemplate, req.Desired.InfrastructureMachineTemplate, infrav1alpha1.GroupVersion.String(), "TartMachineTemplate", []string{imageField, versionField}, []string{imageField, "schematicID"})
	if err != nil {
		return runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, runtimehooksv1.Patch{}, err
	}
	bootstrapPatch, err := planBootstrapConfigTemplatePatch(req.Current.BootstrapConfigTemplate, req.Desired.BootstrapConfigTemplate)
	return machinePatch, infrastructurePatch, bootstrapPatch, err
}

// bootstrapUpdatableSpecPathsは、TartBootstrapConfigのspecのうちin-place updateで変更してよいpathである。
// configPatchesSecretRefが指すimmutable Secretの差し替えによって生じるeffective configuration差分の安全性は、
// Secretの内容を観測できるUpdateMachineでdestructive判定を行って決める。CanUpdateMachineはpolicyだけをfail-closedで確認する。
var bootstrapUpdatableSpecPaths = [][]string{{"configPatchesSecretRef"}, {"updatePolicy"}}

// machineUpdatableSpecPathsは、CAPI Machine specのうちin-place updateで変更してよいpathである。
// versionはTartControlPlaneが実行したcluster-wide Kubernetes upgradeの結果をMachineへ伝播するためだけに許可し、
// このExtensionからupgrade-k8s相当の処理を実行することはない。
var machineUpdatableSpecPaths = [][]string{{versionField}}

// planBootstrapConfigPatchは、TartBootstrapConfigのconfiguration update policyに従ってraw patch参照の変更を許可する。
// apply strategyの変更とSecret参照の変更を同じupdate patchとして扱う。
func planBootstrapConfigPatch(currentRaw, desiredRaw runtime.RawExtension) (runtimehooksv1.Patch, error) {
	err := validateDesiredBootstrapStrategy(desiredRaw, "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	return planRawObjectPatch(currentRaw, desiredRaw, bootstrapv1alpha1.GroupVersion.String(), tartBootstrapConfigKind, bootstrapUpdatableSpecPaths...)
}

// planBootstrapConfigTemplatePatchはMachineSet templateについて同じpolicy判定を行う。
func planBootstrapConfigTemplatePatch(currentRaw, desiredRaw runtime.RawExtension) (runtimehooksv1.Patch, error) {
	err := validateDesiredBootstrapStrategy(desiredRaw, "spec", "template", "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	return planRawTemplatePatch(currentRaw, desiredRaw, bootstrapv1alpha1.GroupVersion.String(), tartBootstrapConfigTemplateKind, bootstrapUpdatableSpecPaths...)
}

// validateDesiredBootstrapStrategyはdesired objectのspecからconfiguration apply strategyを検証する。
// objectが存在しない場合は既定値のRebootとして扱い、解釈できないstrategyはerrorにしてfail-closedへ倒す。
func validateDesiredBootstrapStrategy(desiredRaw runtime.RawExtension, specPath ...string) error {
	object, present, err := decodeRawObject(desiredRaw)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	spec, err := requiredMap(object, specPath...)
	if err != nil {
		return err
	}
	value, exists, err := readPath(spec, []string{"updatePolicy", "configuration"})
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	policy, ok := value.(string)
	if !ok {
		return errors.New("bootstrap configuration apply strategy is not a string")
	}
	switch bootstrapv1alpha1.ConfigurationApplyStrategy(policy) {
	case bootstrapv1alpha1.ConfigurationApplyStrategyStagedReboot, bootstrapv1alpha1.ConfigurationApplyStrategyApplyOnly:
		return nil
	default:
		return errors.New("bootstrap configuration apply strategy is unknown")
	}
}

func planRawObjectPatch(currentRaw, desiredRaw runtime.RawExtension, expectedAPIVersion, expectedKind string, allowedPaths ...[]string) (runtimehooksv1.Patch, error) {
	current, currentPresent, err := decodeRawObject(currentRaw)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	desired, desiredPresent, err := decodeRawObject(desiredRaw)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	if !currentPresent || !desiredPresent {
		if currentPresent == desiredPresent {
			return runtimehooksv1.Patch{}, nil
		}
		return runtimehooksv1.Patch{}, errors.New("optional update object presence changed")
	}
	if err := validateRawObjectIdentity(current, expectedAPIVersion, expectedKind); err != nil {
		return runtimehooksv1.Patch{}, err
	}
	if err := validateRawObjectIdentity(desired, expectedAPIVersion, expectedKind); err != nil {
		return runtimehooksv1.Patch{}, err
	}
	currentSpec, err := requiredMap(current, "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	desiredSpec, err := requiredMap(desired, "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	return planSpecPatch(currentSpec, desiredSpec, allowedPaths, "/spec")
}

func planRawTemplatePatch(currentRaw, desiredRaw runtime.RawExtension, expectedAPIVersion, expectedKind string, allowedPaths ...[]string) (runtimehooksv1.Patch, error) {
	current, currentPresent, err := decodeRawObject(currentRaw)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	desired, desiredPresent, err := decodeRawObject(desiredRaw)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	if !currentPresent || !desiredPresent {
		if currentPresent == desiredPresent {
			return runtimehooksv1.Patch{}, nil
		}
		return runtimehooksv1.Patch{}, errors.New("optional template presence changed")
	}
	if err := validateRawObjectIdentity(current, expectedAPIVersion, expectedKind); err != nil {
		return runtimehooksv1.Patch{}, err
	}
	if err := validateRawObjectIdentity(desired, expectedAPIVersion, expectedKind); err != nil {
		return runtimehooksv1.Patch{}, err
	}
	currentSpec, err := requiredMap(current, "spec", "template", "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	desiredSpec, err := requiredMap(desired, "spec", "template", "spec")
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	return planSpecPatch(currentSpec, desiredSpec, allowedPaths, "/spec/template/spec")
}

func validateRawObjectIdentity(object map[string]any, expectedAPIVersion, expectedKind string) error {
	apiVersion, ok := object["apiVersion"].(string)
	if !ok || apiVersion != expectedAPIVersion {
		return errors.New("update object has an unexpected apiVersion")
	}
	kind, ok := object["kind"].(string)
	if !ok || kind != expectedKind {
		return errors.New("update object has an unexpected kind")
	}
	return nil
}

func planSpecPatch(current, desired map[string]any, allowedPaths [][]string, patchPath string) (runtimehooksv1.Patch, error) {
	if reflect.DeepEqual(current, desired) {
		return runtimehooksv1.Patch{}, nil
	}
	normalized := cloneMap(current)
	for _, path := range allowedPaths {
		if err := copyOrDeletePath(normalized, desired, path); err != nil {
			return runtimehooksv1.Patch{}, err
		}
	}
	if !reflect.DeepEqual(normalized, desired) {
		return runtimehooksv1.Patch{}, errors.New("update contains an unsupported spec difference")
	}
	patch, err := json.Marshal([]jsonPatchOperation{{Operation: "replace", Path: patchPath, Value: desired}})
	if err != nil {
		return runtimehooksv1.Patch{}, fmt.Errorf("encode update patch: %w", err)
	}
	return runtimehooksv1.Patch{PatchType: runtimehooksv1.JSONPatchType, Patch: patch}, nil
}

func copyOrDeletePath(current, desired map[string]any, path []string) error {
	value, exists, err := readPath(desired, path)
	if err != nil {
		return err
	}
	if exists {
		return writePath(current, path, value)
	}
	return deletePath(current, path)
}

func readPath(root map[string]any, path []string) (any, bool, error) {
	var current any = root
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false, errors.New("update path parent is not an object")
		}
		value, exists := object[part]
		if !exists {
			return nil, false, nil
		}
		current = value
	}
	return current, true, nil
}

func writePath(root map[string]any, path []string, value any) error {
	current := root
	for _, part := range path[:len(path)-1] {
		value, exists := current[part]
		if !exists {
			nested := make(map[string]any)
			current[part] = nested
			current = nested
			continue
		}
		nested, ok := value.(map[string]any)
		if !ok {
			return errors.New("update path parent is not an object")
		}
		current = nested
	}
	current[path[len(path)-1]] = cloneJSONValue(value)
	return nil
}

func deletePath(root map[string]any, path []string) error {
	current := root
	for _, part := range path[:len(path)-1] {
		value, exists := current[part]
		if !exists {
			return nil
		}
		nested, ok := value.(map[string]any)
		if !ok {
			return errors.New("update path parent is not an object")
		}
		current = nested
	}
	delete(current, path[len(path)-1])
	return nil
}

func decodeRawObject(raw runtime.RawExtension) (map[string]any, bool, error) {
	data, err := rawBytes(raw)
	if err != nil {
		return nil, false, err
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, false, fmt.Errorf("decode update object: %w", err)
	}
	if object == nil {
		return nil, false, errors.New("update object is not a JSON object")
	}
	return object, true, nil
}

func rawBytes(raw runtime.RawExtension) ([]byte, error) {
	if len(raw.Raw) > 0 {
		return raw.Raw, nil
	}
	if raw.Object == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw.Object)
	if err != nil {
		return nil, fmt.Errorf("encode update object: %w", err)
	}
	return data, nil
}

func validateTemplateImage(raw runtime.RawExtension) error {
	object, present, err := decodeRawObject(raw)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("update object is missing its infrastructure template")
	}
	spec, err := requiredMap(object, "spec", "template", "spec")
	if err != nil {
		return err
	}
	value, exists, err := readPath(spec, []string{imageField})
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("update object is missing its Talos image")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Talos image: %w", err)
	}
	var image infrav1alpha1.TalosImageSpec
	if err := json.Unmarshal(data, &image); err != nil {
		return fmt.Errorf("decode Talos image: %w", err)
	}
	_, err = talos.InstallerImage(image.Version, image.SchematicID)
	return err
}

func marshalMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("update spec is not a JSON object")
	}
	return result, nil
}

func requiredMap(root map[string]any, path ...string) (map[string]any, error) {
	value, exists, err := readPath(root, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("update object is missing its spec")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("update object spec is not an object")
	}
	return object, nil
}

func cloneMap(value map[string]any) map[string]any {
	return cloneJSONValue(value).(map[string]any)
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = cloneJSONValue(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			result[index] = cloneJSONValue(nested)
		}
		return result
	default:
		return value
	}
}
