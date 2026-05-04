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
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	k8shost "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/host"
	applicationprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/provisioning"
)

const tartMachineHostCleanupFinalizerName = "infrastructure.cluster.x-k8s.io/tartmachine-host-cleanup"

var _ = Describe("TartMachine Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const hostName = "test-host"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		tartmachine := &infrastructurev1alpha1.TartMachine{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TartMachine")
			err := k8sClient.Get(ctx, typeNamespacedName, tartmachine)
			if err != nil && k8serrors.IsNotFound(err) {
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
			if err != nil && k8serrors.IsNotFound(err) {
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
			controllerReconciler := newTartMachineReconciler(k8sClient, k8sClient.Scheme(), wolSender)

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
			machine.Status.BootstrapToken = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz012345678901"
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
			controllerReconciler := newTartMachineReconciler(k8sClient, k8sClient.Scheme(), wolSender)

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

	Context("When deleting a TartMachine after its name was reused", func() {
		const resourceName = "test-reused-machine"
		const hostName = "test-reused-machine-host"

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
					MACAddress: "00:11:22:33:44:98",
				},
			}
			Expect(k8sClient.Create(ctx, host)).To(Succeed())

			host.Status.State = infrastructurev1alpha1.TartHostStateReserved
			host.Status.MachineRef = &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  machine.Namespace,
				Name:       machine.Name,
				UID:        "stale-machine-uid",
			}
			Expect(k8sClient.Status().Update(ctx, host)).To(Succeed())

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

		It("should not release a host that references another machine UID", func() {
			machine := &infrastructurev1alpha1.TartMachine{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, machine)).To(Succeed())

			Expect(k8shost.NewService(k8sClient).ReleaseAssigned(ctx, machine)).To(Succeed())

			updatedHost := &infrastructurev1alpha1.TartHost{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, updatedHost)).To(Succeed())
			Expect(updatedHost.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateReserved))
			Expect(updatedHost.Status.MachineRef).NotTo(BeNil())
			Expect(updatedHost.Status.MachineRef.UID).To(Equal(types.UID("stale-machine-uid")))
		})
	})

	Context("When BootstrapToken is consumed after metadata delivery", func() {
		const resourceName = "test-provisioned-resource"
		const hostName = "test-provisioned-host"

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
					MACAddress:     "00:11:22:33:44:99",
					BootMACAddress: "00:11:22:33:44:88",
				},
			}
			Expect(k8sClient.Create(ctx, host)).To(Succeed())

			host.Status.State = infrastructurev1alpha1.TartHostStateProvisioning
			host.Status.MachineRef = &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  machine.Namespace,
				Name:       machine.Name,
				UID:        machine.UID,
			}
			Expect(k8sClient.Status().Update(ctx, host)).To(Succeed())

			machine.Status.HostRef = &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartHost",
				Namespace:  host.Namespace,
				Name:       host.Name,
				UID:        host.UID,
			}
			machine.Status.BootstrapToken = ""
			Expect(k8sClient.Status().Update(ctx, machine)).To(Succeed())
		})

		AfterEach(func() {
			cleanupTartMachine(ctx, typeNamespacedName)

			host := &infrastructurev1alpha1.TartHost{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, host); err == nil {
				Expect(k8sClient.Delete(ctx, host)).To(Succeed())
			}
		})

		It("should set TartMachine Ready=true and transition TartHost to Provisioned", func() {
			wolSender := &fakeWakeOnLANSender{}
			controllerReconciler := newTartMachineReconciler(k8sClient, k8sClient.Scheme(), wolSender)

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updatedMachine := &infrastructurev1alpha1.TartMachine{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedMachine)).To(Succeed())
			Expect(updatedMachine.Status.Ready).To(BeTrue())

			updatedHost := &infrastructurev1alpha1.TartHost{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, updatedHost)).To(Succeed())
			Expect(updatedHost.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateProvisioned))
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
			controllerReconciler := newTartMachineReconciler(k8sClient, k8sClient.Scheme(), wolSender)

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
			controllerReconciler := newTartMachineReconciler(k8sClient, k8sClient.Scheme(), &fakeWakeOnLANSender{})

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
				g.Expect(k8serrors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})

	Context("When BootstrapToken has expired (TokenExpiresAt in the past)", func() {
		const resourceName = "test-expired-token-resource"
		const hostName = "test-expired-token-host"

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
					MACAddress:     "00:11:22:33:44:aa",
					BootMACAddress: "00:11:22:33:44:bb",
				},
			}
			Expect(k8sClient.Create(ctx, host)).To(Succeed())

			// ホストを Reserved 状態に設定
			host.Status.State = infrastructurev1alpha1.TartHostStateReserved
			host.Status.MachineRef = &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  machine.Namespace,
				Name:       machine.Name,
				UID:        machine.UID,
			}
			Expect(k8sClient.Status().Update(ctx, host)).To(Succeed())

			// machine の status を設定（トークン期限が過去）
			pastTime := metav1.NewTime(time.Now().Add(-11 * time.Minute))
			machine.Status.HostRef = &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartHost",
				Namespace:  host.Namespace,
				Name:       host.Name,
				UID:        host.UID,
			}
			machine.Status.BootstrapToken = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz012345678901"
			machine.Status.TokenExpiresAt = &pastTime
			Expect(k8sClient.Status().Update(ctx, machine)).To(Succeed())
		})

		AfterEach(func() {
			cleanupTartMachine(ctx, typeNamespacedName)

			host := &infrastructurev1alpha1.TartHost{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, host); err == nil {
				Expect(k8sClient.Delete(ctx, host)).To(Succeed())
			}
		})

		It("should regenerate a new bootstrap token, re-send WoL, and transition host to Provisioning", func() {
			wolSender := &fakeWakeOnLANSender{}
			controllerReconciler := newTartMachineReconciler(k8sClient, k8sClient.Scheme(), wolSender)

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updatedMachine := &infrastructurev1alpha1.TartMachine{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedMachine)).To(Succeed())
			Expect(updatedMachine.Status.HostRef).NotTo(BeNil())
			Expect(updatedMachine.Status.HostRef.Name).To(Equal(hostName))
			Expect(updatedMachine.Status.BootstrapToken).To(MatchRegexp(`^[A-Za-z0-9]{64}$`))
			Expect(updatedMachine.Status.TokenExpiresAt).NotTo(BeNil())
			Expect(updatedMachine.Status.TokenExpiresAt.After(time.Now())).To(BeTrue())
			Expect(updatedMachine.Status.Ready).To(BeFalse())
			Expect(apimeta.IsStatusConditionTrue(updatedMachine.Status.Conditions, "Provisioning")).To(BeFalse())
			provisioningCond := apimeta.FindStatusCondition(updatedMachine.Status.Conditions, "Provisioning")
			Expect(provisioningCond).NotTo(BeNil())
			Expect(provisioningCond.Reason).To(Equal("TokenExpired"))

			updatedHost := &infrastructurev1alpha1.TartHost{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, updatedHost)).To(Succeed())
			Expect(updatedHost.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateProvisioning))
			Expect(wolSender.sentMACAddresses).To(ContainElement("00:11:22:33:44:bb"))
		})
	})

	Context("When MarkProvisioning fails with a non-conflict error", func() {
		const resourceName = "test-mark-provisioning-failure"
		const hostName = "test-mark-provisioning-failure-host"

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
					MACAddress:     "00:11:22:33:44:cc",
					BootMACAddress: "00:11:22:33:44:dd",
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

		It("should return the error from the provisioning service", func() {
			failingProvisioning := &failingProvisioningService{
				beginErr: fmt.Errorf("mark provisioning failed"),
			}
			controllerReconciler := &TartMachineReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				HostService:  k8shost.NewService(k8sClient),
				Provisioning: failingProvisioning,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mark provisioning failed"))
		})
	})

	Context("When no available hosts exist", func() {
		const resourceName = "test-no-hosts"

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
		})

		AfterEach(func() {
			cleanupTartMachine(ctx, typeNamespacedName)
		})

		It("should set HostReserved condition with NoAvailableHost reason", func() {
			controllerReconciler := newTartMachineReconciler(k8sClient, k8sClient.Scheme(), &fakeWakeOnLANSender{})

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1alpha1.TartMachine{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			condition := apimeta.FindStatusCondition(updated.Status.Conditions, "HostReserved")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("NoAvailableHost"))
		})
	})

	Context("When Begin fails after WoL is sent", func() {
		const resourceName = "test-begin-failure"
		const hostName = "test-begin-failure-host"

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
					MACAddress: "00:11:22:33:44:ee",
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

		It("should leave host in Reserved state and return error", func() {
			failingProvisioning := &failingProvisioningService{
				beginErr: fmt.Errorf("begin failed"),
			}
			controllerReconciler := &TartMachineReconciler{
				Client:       k8sClient,
				Scheme:       k8sClient.Scheme(),
				HostService:  k8shost.NewService(k8sClient),
				Provisioning: failingProvisioning,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("begin failed"))

			updatedHost := &infrastructurev1alpha1.TartHost{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: hostName, Namespace: "default"}, updatedHost)).To(Succeed())
			Expect(updatedHost.Status.State).To(Equal(infrastructurev1alpha1.TartHostStateReserved))
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

func newTartMachineReconciler(k8sClient client.Client, scheme *runtime.Scheme, wolSender applicationprovisioning.WakeOnLANSender) *TartMachineReconciler {
	hostService := k8shost.NewService(k8sClient)
	return &TartMachineReconciler{
		Client:       k8sClient,
		Scheme:       scheme,
		HostService:  hostService,
		Provisioning: applicationprovisioning.NewService(hostService, hostService, wolSender),
	}
}

func cleanupTartMachine(ctx context.Context, key types.NamespacedName) {
	machine := &infrastructurev1alpha1.TartMachine{}
	if err := k8sClient.Get(ctx, key, machine); k8serrors.IsNotFound(err) {
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
		if k8serrors.IsNotFound(err) {
			return
		}
		g.Expect(err).NotTo(HaveOccurred())
		current.Finalizers = nil
		g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
	}).Should(Succeed())
}

type failingProvisioningService struct {
	beginErr error
}

func (s *failingProvisioningService) Begin(ctx context.Context, host *infrastructurev1alpha1.TartHost) error {
	if s.beginErr != nil {
		return s.beginErr
	}
	return nil
}

func (s *failingProvisioningService) Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	return nil
}

