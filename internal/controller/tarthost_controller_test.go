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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
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

	Context("When a reserved host points to a TartMachine with the same name but different UID", func() {
		It("should release the stale reference and report that it was released", func() {
			testScheme := runtime.NewScheme()
			Expect(scheme.AddToScheme(testScheme)).To(Succeed())
			Expect(infrastructurev1alpha1.AddToScheme(testScheme)).To(Succeed())

			machine := &infrastructurev1alpha1.TartMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reused-machine",
					Namespace: "default",
					UID:       "current-machine-uid",
				},
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "https://assets.example.invalid/images/talos.raw",
				},
			}
			host := &infrastructurev1alpha1.TartHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stale-host",
					Namespace: "default",
				},
				Spec: infrastructurev1alpha1.TartHostSpec{
					MACAddress: "00:11:22:33:44:97",
				},
				Status: infrastructurev1alpha1.TartHostStatus{
					State: infrastructurev1alpha1.TartHostStateReserved,
					MachineRef: &corev1.ObjectReference{
						APIVersion: infrastructurev1alpha1.GroupVersion.String(),
						Kind:       "TartMachine",
						Namespace:  "default",
						Name:       "reused-machine",
						UID:        "stale-machine-uid",
					},
				},
			}

			cl := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&infrastructurev1alpha1.TartHost{}).
				WithObjects(machine, host).
				Build()

			released, err := hostdomain.NewService(cl).ReleaseMissingReference(context.Background(), host)
			Expect(err).NotTo(HaveOccurred())
			Expect(released).To(BeTrue())

			updatedHost := &infrastructurev1alpha1.TartHost{}
			Expect(cl.Get(context.Background(), types.NamespacedName{Name: "stale-host", Namespace: "default"}, updatedHost)).To(Succeed())
			Expect(updatedHost.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateAvailable))
			Expect(updatedHost.Status.MachineRef).To(BeNil())
		})
	})

	Context("MachineRef index helper", func() {
		It("should build a unique index key from namespace, name, and UID", func() {
			ref := &corev1.ObjectReference{
				Namespace: "default",
				Name:      "machine-a",
				UID:       "machine-uid",
			}

			Expect(tartHostMachineRefIndexValue(ref)).To(Equal("default/machine-a/machine-uid"))
			Expect(IndexTartHostByMachineRef(&infrastructurev1alpha1.TartHost{
				Status: infrastructurev1alpha1.TartHostStatus{MachineRef: ref},
			})).To(Equal([]string{"default/machine-a/machine-uid"}))
		})
	})
})
