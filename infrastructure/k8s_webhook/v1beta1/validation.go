// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1beta1

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/ocireference"
)

var (
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
)

func validateArtifactPolicy(policy infrastructurev1beta1.ArtifactPolicy, path *field.Path) field.ErrorList {
	var errors field.ErrorList
	if len(policy.AllowedRegistries) == 0 {
		errors = append(errors, field.Required(path.Child("allowedRegistries"), "at least one registry is required"))
		return errors
	}
	for index, registry := range policy.AllowedRegistries {
		if err := validateRegistry(registry); err != nil {
			errors = append(errors, field.Invalid(path.Child("allowedRegistries").Index(index), registry, err.Error()))
		}
	}
	return errors
}

func validateRegistry(registry string) error {
	if strings.ContainsAny(registry, "*/ \t\r\n") {
		return fmt.Errorf("must be a hostname or hostname:port without wildcard or path")
	}

	host := registry
	if strings.Contains(registry, ":") {
		var port string
		var err error
		host, port, err = net.SplitHostPort(registry)
		if err != nil {
			return fmt.Errorf("must contain a valid hostname:port pair")
		}
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return fmt.Errorf("port must be between 1 and 65535")
		}
	}
	if len(host) > 253 || !hostnamePattern.MatchString(host) || strings.Contains(host, "..") {
		return fmt.Errorf("must contain a valid hostname")
	}
	return nil
}

func validateImage(image infrastructurev1beta1.ImageSpec, path *field.Path) field.ErrorList {
	if _, err := ocireference.Parse(image.Ref); err != nil {
		return field.ErrorList{field.Invalid(path.Child("ref"), image.Ref, "must be a valid OCI image reference")}
	}
	return nil
}

func validateResourceReference(ref infrastructurev1beta1.ResourceReference, path *field.Path) field.ErrorList {
	var errors field.ErrorList
	if ref.Namespace == "" {
		errors = append(errors, field.Required(path.Child("namespace"), "namespace is required"))
	}
	if ref.Name == "" {
		errors = append(errors, field.Required(path.Child("name"), "name is required"))
	}
	if ref.UID == "" {
		errors = append(errors, field.Required(path.Child("uid"), "UID is required"))
	}
	return errors
}

func validateDigest(value string, path *field.Path) field.ErrorList {
	if !digestPattern.MatchString(value) {
		return field.ErrorList{field.Invalid(path, value, "must use sha256:<64 lowercase hexadecimal characters>")}
	}
	return nil
}

func validateOperationSpec(spec infrastructurev1beta1.TartHostOperationSpec) field.ErrorList {
	specPath := field.NewPath("spec")
	var errors field.ErrorList
	if _, err := operationdomain.ParseID(spec.OperationID); err != nil {
		errors = append(errors, field.Invalid(specPath.Child("operationID"), spec.OperationID, "must be a non-zero UUID"))
	}
	errors = append(errors, validateResourceReference(spec.HostRef, specPath.Child("hostRef"))...)
	errors = append(errors, validateDigest(spec.PlanDigest, specPath.Child("planDigest"))...)
	errors = append(errors, validateDigest(spec.DesiredObjectsDigest, specPath.Child("desiredObjectsDigest"))...)
	if spec.Deadline.IsZero() {
		errors = append(errors, field.Required(specPath.Child("deadline"), "deadline is required"))
	}

	if spec.Type == infrastructurev1beta1.OperationTypeWipeAll {
		if spec.MachineRef != nil {
			errors = append(errors, field.Forbidden(specPath.Child("machineRef"), "must be omitted for a manual WipeAll operation"))
		}
	} else if spec.MachineRef == nil {
		errors = append(errors, field.Required(specPath.Child("machineRef"), "machineRef is required for this operation type"))
	}
	if spec.MachineRef != nil {
		errors = append(errors, validateResourceReference(*spec.MachineRef, specPath.Child("machineRef"))...)
	}

	if spec.Type == infrastructurev1beta1.OperationTypeUpdate {
		if spec.TargetSlot == "" {
			errors = append(errors, field.Required(specPath.Child("targetSlot"), "targetSlot is required for Update"))
		}
		if spec.TargetImageDigest == "" {
			errors = append(errors, field.Required(specPath.Child("targetImageDigest"), "targetImageDigest is required for Update"))
		} else {
			errors = append(errors, validateDigest(spec.TargetImageDigest, specPath.Child("targetImageDigest"))...)
		}
		if spec.TargetArtifactGeneration == nil {
			errors = append(errors, field.Required(specPath.Child("targetArtifactGeneration"), "targetArtifactGeneration is required for Update"))
		}
		if spec.UpdateClass == "" {
			errors = append(errors, field.Required(specPath.Child("updateClass"), "updateClass is required for Update"))
		}
	}
	return errors
}
