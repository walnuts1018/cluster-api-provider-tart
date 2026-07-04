package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
)

func TestManagedByV1Beta1(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{name: "annotationなし"},
		{
			name: "変換データあり",
			annotations: map[string]string{
				utilconversion.DataAnnotation: `{"spec":{}}`,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := &metav1.PartialObjectMetadata{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
			}
			if got := managedByV1Beta1(object); got != tt.want {
				t.Fatalf("managedByV1Beta1() = %t, want %t", got, tt.want)
			}
		})
	}
}
