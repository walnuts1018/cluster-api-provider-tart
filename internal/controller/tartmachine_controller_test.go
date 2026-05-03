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
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

const tartMachineHostCleanupFinalizerName = "infrastructure.cluster.x-k8s.io/tartmachine-host-cleanup"

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
			cleanupTartMachine(ctx, typeNamespacedName)

			host := &infrastructurev1alpha1.TartHost{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, host)
			if err == nil {
				Expect(k8sClient.Delete(ctx, host)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			wolSender := &fakeWakeOnLANSender{}
			controllerReconciler := &TartMachineReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				WakeOnLANSender: wolSender,
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
			Expect(updated.Status.ProvisioningStartTime).NotTo(BeNil())

			updatedHost := &infrastructurev1alpha1.TartHost{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, updatedHost)).To(Succeed())
			Expect(updatedHost.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateProvisioning))
			Expect(wolSender.sentMACAddresses).To(ContainElement("00:11:22:33:44:66"))
		})
	})

	Context("When machine has HostRef but host is still Reserved (retry after partial failure)", func() {
		const resourceName = "test-retry-resource"
		const hostName = "test-retry-host"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			machine := &infrastructurev1alpha1.TartMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "https://assets.example.invalid/images/talos.raw",
				},
			}
			Expect(k8sClient.Create(ctx, machine)).To(Succeed())

			host := &infrastructurev1alpha1.TartHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hostName,
					Namespace: "default",
				},
				Spec: infrastructurev1alpha1.TartHostSpec{
					MACAddress: "00:11:22:33:44:99",
				},
			}
			Expect(k8sClient.Create(ctx, host)).To(Succeed())

			// ホストを Reserved 状態に設定（WoL 送信後のマシン status patch が失敗した状態を再現）
			host.Status.State = infrastructurev1alpha1.TartHostStateReserved
			host.Status.MachineRef = &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  machine.Namespace,
				Name:       machine.Name,
				UID:        machine.UID,
			}
			Expect(k8sClient.Status().Update(ctx, host)).To(Succeed())

			// machine.Status.HostRef を設定（HostRef は書き込み済みだが Provisioning 遷移が未完了の状態）
			machine.Status.HostRef = &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartHost",
				Namespace:  host.Namespace,
				Name:       host.Name,
				UID:        host.UID,
			}
			Expect(k8sClient.Status().Update(ctx, machine)).To(Succeed())
		})

		AfterEach(func() {
			cleanupTartMachine(ctx, typeNamespacedName)

			host := &infrastructurev1alpha1.TartHost{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, host); err == nil {
				Expect(k8sClient.Delete(ctx, host)).To(Succeed())
			}
		})

		It("should resend Wake-on-LAN and transition host to Provisioning", func() {
			wolSender := &fakeWakeOnLANSender{}
			controllerReconciler := &TartMachineReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				WakeOnLANSender: wolSender,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updatedHost := &infrastructurev1alpha1.TartHost{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, updatedHost)).To(Succeed())
			Expect(updatedHost.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateProvisioning))
			Expect(wolSender.sentMACAddresses).To(ContainElement("00:11:22:33:44:99"))
		})
	})

	Context("When a TartHost has a dedicated boot MAC address", func() {
		const resourceName = "test-boot-mac-resource"
		const hostName = "test-boot-mac-host"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			machine := &infrastructurev1alpha1.TartMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "https://assets.example.invalid/images/talos.raw",
				},
			}
			Expect(k8sClient.Create(ctx, machine)).To(Succeed())

			host := &infrastructurev1alpha1.TartHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hostName,
					Namespace: "default",
				},
				Spec: infrastructurev1alpha1.TartHostSpec{
					MACAddress:     "00:11:22:33:44:77",
					BootMACAddress: "00:11:22:33:44:88",
				},
			}
			Expect(k8sClient.Create(ctx, host)).To(Succeed())
			host.Status.State = infrastructurev1alpha1.TartHostStateAvailable
			Expect(k8sClient.Status().Update(ctx, host)).To(Succeed())
		})

		AfterEach(func() {
			cleanupTartMachine(ctx, typeNamespacedName)

			host := &infrastructurev1alpha1.TartHost{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, host); err == nil {
				Expect(k8sClient.Delete(ctx, host)).To(Succeed())
			}
		})

		It("should send Wake-on-LAN to boot MAC address", func() {
			wolSender := &fakeWakeOnLANSender{}
			controllerReconciler := &TartMachineReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				WakeOnLANSender: wolSender,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(wolSender.sentMACAddresses).To(ContainElement("00:11:22:33:44:88"))
			Expect(slices.Contains(wolSender.sentMACAddresses, "00:11:22:33:44:77")).To(BeFalse())
		})
	})

	Context("When deleting a TartMachine with an assigned host", func() {
		const resourceName = "test-delete-resource"
		const hostName = "test-delete-host"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			machine := &infrastructurev1alpha1.TartMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "https://assets.example.invalid/images/talos.raw",
				},
			}
			Expect(k8sClient.Create(ctx, machine)).To(Succeed())

			host := &infrastructurev1alpha1.TartHost{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hostName,
					Namespace: "default",
				},
				Spec: infrastructurev1alpha1.TartHostSpec{
					MACAddress: "00:11:22:33:44:aa",
				},
			}
			Expect(k8sClient.Create(ctx, host)).To(Succeed())
			host.Status.State = infrastructurev1alpha1.TartHostStateAvailable
			Expect(k8sClient.Status().Update(ctx, host)).To(Succeed())
		})

		AfterEach(func() {
			cleanupTartMachine(ctx, typeNamespacedName)

			host := &infrastructurev1alpha1.TartHost{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, host); err == nil {
				Expect(k8sClient.Delete(ctx, host)).To(Succeed())
			}
		})

		It("should release the assigned host before removing the finalizer", func() {
			controllerReconciler := &TartMachineReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				WakeOnLANSender: &fakeWakeOnLANSender{},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			machine := &infrastructurev1alpha1.TartMachine{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, machine)).To(Succeed())
			Expect(machine.Finalizers).To(ContainElement(tartMachineHostCleanupFinalizerName))

			Expect(k8sClient.Delete(ctx, machine)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				host := &infrastructurev1alpha1.TartHost{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, host)).To(Succeed())
				g.Expect(host.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateAvailable))
				g.Expect(host.Status.MachineRef).To(BeNil())

				err := k8sClient.Get(ctx, typeNamespacedName, &infrastructurev1alpha1.TartMachine{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})
})

type fakeWakeOnLANSender struct {
	sentMACAddresses []string
}

func (s *fakeWakeOnLANSender) Send(macAddress string) error {
	s.sentMACAddresses = append(s.sentMACAddresses, macAddress)
	return nil
}

func cleanupTartMachine(ctx context.Context, key types.NamespacedName) {
	machine := &infrastructurev1alpha1.TartMachine{}
	if err := k8sClient.Get(ctx, key, machine); errors.IsNotFound(err) {
		return
	} else {
		Expect(err).NotTo(HaveOccurred())
	}

	Expect(k8sClient.Delete(ctx, machine)).To(Succeed())

	if len(machine.Finalizers) == 0 {
		return
	}

	Eventually(func(g Gomega) {
		current := &infrastructurev1alpha1.TartMachine{}
		err := k8sClient.Get(ctx, key, current)
		if errors.IsNotFound(err) {
			return
		}
		g.Expect(err).NotTo(HaveOccurred())
		current.Finalizers = nil
		g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
	}).Should(Succeed())
}
