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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/resource_finalizer"
)

var _ = Describe("TartCluster Controller", func() {
	const clusterName = "test-cluster"
	const tartClusterName = "test-tart-cluster"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      tartClusterName,
		Namespace: "default",
	}

	BeforeEach(func() {
		capiCluster := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: "default",
				Labels: map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				},
			},
			Spec: clusterv1.ClusterSpec{
				Paused: new(bool),
			},
		}
		Expect(k8sClient.Create(ctx, capiCluster)).To(Succeed())

		tartCluster := &infrastructurev1beta1.TartCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tartClusterName,
				Namespace: "default",
				Labels: map[string]string{
					clusterv1.ClusterNameLabel: clusterName,
				},
			},
			Spec: infrastructurev1beta1.TartClusterSpec{
				ArtifactPolicy: infrastructurev1beta1.ArtifactPolicy{
					AllowedRegistries: []string{"registry.sample.walnuts.dev"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, tartCluster)).To(Succeed())
	})

	AfterEach(func() {
		tartCluster := &infrastructurev1beta1.TartCluster{}
		if err := k8sClient.Get(ctx, typeNamespacedName, tartCluster); err == nil {
			controllerutil.RemoveFinalizer(tartCluster, resourcefinalizer.TartClusterFinalizer)
			Expect(k8sClient.Update(ctx, tartCluster)).To(Succeed())
			Expect(k8sClient.Delete(ctx, tartCluster)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, tartCluster)
			}, 5*time.Second, time.Millisecond*100).ShouldNot(Succeed())
		}

		capiCluster := &clusterv1.Cluster{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: "default"}, capiCluster); err == nil {
			Expect(k8sClient.Delete(ctx, capiCluster)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: "default"}, capiCluster)
			}, 5*time.Second, time.Millisecond*100).ShouldNot(Succeed())
		}
	})

	It("should successfully reconcile and set status when cluster is not paused", func() {
		controllerReconciler := &TartClusterReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &infrastructurev1beta1.TartCluster{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
		Expect(updated.Status.Initialization.Provisioned).NotTo(BeNil())
		Expect(*updated.Status.Initialization.Provisioned).To(BeTrue())
	})

	Context("When the associated Cluster has spec.paused=true", func() {
		BeforeEach(func() {
			capiCluster := &clusterv1.Cluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: "default"}, capiCluster)).To(Succeed())
			capiCluster.Spec.Paused = new(true)
			Expect(k8sClient.Update(ctx, capiCluster)).To(Succeed())
		})

		It("should skip reconciliation and not update status", func() {
			controllerReconciler := &TartClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1beta1.TartCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.Initialization.Provisioned).To(BeNil())
		})
	})

	Context("When the associated Cluster has the paused annotation", func() {
		BeforeEach(func() {
			capiCluster := &clusterv1.Cluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: "default"}, capiCluster)).To(Succeed())
			if capiCluster.Annotations == nil {
				capiCluster.Annotations = make(map[string]string)
			}
			capiCluster.Annotations[clusterv1.PausedAnnotation] = ""
			Expect(k8sClient.Update(ctx, capiCluster)).To(Succeed())
		})

		It("should skip reconciliation and not update status", func() {
			controllerReconciler := &TartClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1beta1.TartCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.Initialization.Provisioned).To(BeNil())
		})
	})

	Context("When the finalizer is being added", func() {
		It("should add the finalizer to the TartCluster", func() {
			tartCluster := &infrastructurev1beta1.TartCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tartCluster)).To(Succeed())
			Expect(tartCluster.Finalizers).NotTo(ContainElement(resourcefinalizer.TartClusterFinalizer))

			controllerReconciler := &TartClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &infrastructurev1beta1.TartCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(resourcefinalizer.TartClusterFinalizer))
		})
	})

	Context("When deleting a TartCluster", func() {
		BeforeEach(func() {
			tartCluster := &infrastructurev1beta1.TartCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tartCluster)).To(Succeed())
			tartCluster.Finalizers = []string{resourcefinalizer.TartClusterFinalizer}
			Expect(k8sClient.Update(ctx, tartCluster)).To(Succeed())
		})

		It("should remove the finalizer", func() {
			controllerReconciler := &TartClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			tartCluster := &infrastructurev1beta1.TartCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tartCluster)).To(Succeed())
			Expect(k8sClient.Delete(ctx, tartCluster)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, &infrastructurev1beta1.TartCluster{})
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("When TartCluster is missing the cluster label", func() {
		BeforeEach(func() {
			tartCluster := &infrastructurev1beta1.TartCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tartCluster)).To(Succeed())
			delete(tartCluster.Labels, clusterv1.ClusterNameLabel)
			Expect(k8sClient.Update(ctx, tartCluster)).To(Succeed())
		})

		It("should skip reconciliation without error", func() {
			controllerReconciler := &TartClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
