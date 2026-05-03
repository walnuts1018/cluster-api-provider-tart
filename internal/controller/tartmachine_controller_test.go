/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

var _ = Describe("TartMachine Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const hostName = "test-host"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		tartmachine := &infrastructurev1alpha1.TartMachine{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TartMachine")
			err := k8sClient.Get(ctx, typeNamespacedName, tartmachine)
			if err != nil && errors.IsNotFound(err) {
				resource := &infrastructurev1alpha1.TartMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: infrastructurev1alpha1.TartMachineSpec{
						Image: "https://assets.example.invalid/images/talos.raw",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}

			host := &infrastructurev1alpha1.TartHost{}
			hostKey := types.NamespacedName{Name: hostName, Namespace: "default"}
			err = k8sClient.Get(ctx, hostKey, host)
			if err != nil && errors.IsNotFound(err) {
				host = &infrastructurev1alpha1.TartHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      hostName,
						Namespace: "default",
					},
					Spec: infrastructurev1alpha1.TartHostSpec{
						MACAddress: "00:11:22:33:44:66",
					},
				}
				Expect(k8sClient.Create(ctx, host)).To(Succeed())
				host.Status.State = infrastructurev1alpha1.TartHostStateAvailable
				Expect(k8sClient.Status().Update(ctx, host)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &infrastructurev1alpha1.TartMachine{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance TartMachine")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			host := &infrastructurev1alpha1.TartHost{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, host)
			if err == nil {
				Expect(k8sClient.Delete(ctx, host)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TartMachineReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1alpha1.TartMachine{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.HostRef).NotTo(BeNil())
			Expect(updated.Status.HostRef.Name).To(Equal(hostName))
			Expect(updated.Status.BootstrapToken).To(MatchRegexp(`^[A-Za-z0-9]{64}$`))
			Expect(updated.Status.TokenExpiresAt).NotTo(BeNil())
		})
	})
})
