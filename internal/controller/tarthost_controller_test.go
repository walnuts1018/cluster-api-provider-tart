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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

var _ = Describe("TartHost Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		tarthost := &infrastructurev1alpha1.TartHost{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TartHost")
			err := k8sClient.Get(ctx, typeNamespacedName, tarthost)
			if err != nil && errors.IsNotFound(err) {
				resource := &infrastructurev1alpha1.TartHost{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: infrastructurev1alpha1.TartHostSpec{
						MACAddress: "00:11:22:33:44:55",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &infrastructurev1alpha1.TartHost{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance TartHost")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TartHostReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1alpha1.TartHost{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateAvailable))
		})
	})

	Context("When a reserved host points to a missing TartMachine", func() {
		const resourceName = "test-orphan-host"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			host := &infrastructurev1alpha1.TartHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: infrastructurev1alpha1.TartHostSpec{
					MACAddress: "00:11:22:33:44:99",
				},
			}
			Expect(k8sClient.Create(ctx, host)).To(Succeed())
			host.Status.State = infrastructurev1alpha1.TartHostStateReserved
			host.Status.MachineRef = &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  "default",
				Name:       "missing-machine",
			}
			Expect(k8sClient.Status().Update(ctx, host)).To(Succeed())
		})

		AfterEach(func() {
			resource := &infrastructurev1alpha1.TartHost{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reset the host to Available", func() {
			controllerReconciler := &TartHostReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1alpha1.TartHost{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateAvailable))
			Expect(updated.Status.MachineRef).To(BeNil())
		})
	})
})
