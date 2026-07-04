package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
)

func managedByV1Beta1(object metav1.Object) bool {
	_, ok := object.GetAnnotations()[utilconversion.DataAnnotation]
	return ok
}
