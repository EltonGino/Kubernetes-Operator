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

	storagev1alpha1 "github.com/EltonGino/Kubernetes-Operator/api/v1alpha1"
	"github.com/EltonGino/Kubernetes-Operator/internal/bucket"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("CloudBucket Controller", func() {
	const namespace = "default"

	ctx := context.Background()
	reconciler := &CloudBucketReconciler{}

	BeforeEach(func() {
		reconciler = &CloudBucketReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			BucketService: bucket.NewFakeService(),
		}
	})

	AfterEach(func() {
		cleanupCloudBuckets(ctx, namespace)
		cleanupSecrets(ctx, namespace)
	})

	It("sets Ready false when the credentials Secret is missing", func() {
		cloudBucket := newCloudBucket("missing-secret-bucket", "missing-secret-creds")
		Expect(k8sClient.Create(ctx, cloudBucket)).To(Succeed())

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(cloudBucket),
		})
		Expect(err).NotTo(HaveOccurred())

		actual := &storagev1alpha1.CloudBucket{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), actual)).To(Succeed())
		Expect(actual.Finalizers).To(ContainElement(cloudBucketFinalizer))
		Expect(actual.Status.ObservedGeneration).To(Equal(actual.Generation))
		Expect(actual.Status.Provider).To(Equal(defaultProvider))
		Expect(actual.Status.Region).To(Equal(defaultRegion))
		Expect(actual.Status.ActualBucketName).To(BeEmpty())
		Expect(actual.Status.Endpoint).To(BeEmpty())

		ready := meta.FindStatusCondition(actual.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(reasonCredentialsMissing))

		credentials := meta.FindStatusCondition(actual.Status.Conditions, conditionCredentialsAvailable)
		Expect(credentials).NotTo(BeNil())
		Expect(credentials.Status).To(Equal(metav1.ConditionFalse))
		Expect(credentials.Reason).To(Equal(reasonCredentialsMissing))
	})

	It("sets Ready false when the MinIO credentials Secret is invalid", func() {
		realServiceReconciler := &CloudBucketReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		cloudBucket := newCloudBucket("invalid-secret-bucket", "invalid-secret-creds")
		secret := credentialsSecret(cloudBucket.Spec.CredentialsSecretName)
		delete(secret.StringData, "secretKey")

		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		Expect(k8sClient.Create(ctx, cloudBucket)).To(Succeed())

		_, err := realServiceReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(cloudBucket),
		})
		Expect(err).NotTo(HaveOccurred())

		actual := &storagev1alpha1.CloudBucket{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), actual)).To(Succeed())
		Expect(actual.Finalizers).To(ContainElement(cloudBucketFinalizer))

		ready := meta.FindStatusCondition(actual.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(reasonCredentialsInvalid))
		Expect(ready.Message).NotTo(ContainSubstring("fake-secret-key"))

		credentials := meta.FindStatusCondition(actual.Status.Conditions, conditionCredentialsAvailable)
		Expect(credentials).NotTo(BeNil())
		Expect(credentials.Status).To(Equal(metav1.ConditionFalse))
		Expect(credentials.Reason).To(Equal(reasonCredentialsInvalid))
		Expect(credentials.Message).To(ContainSubstring(`missing required key "secretKey"`))
	})

	It("uses the fake bucket service to mark a bucket ready", func() {
		cloudBucket := newCloudBucket("ready-bucket", "ready-bucket-creds")
		Expect(k8sClient.Create(ctx, credentialsSecret(cloudBucket.Spec.CredentialsSecretName))).To(Succeed())
		Expect(k8sClient.Create(ctx, cloudBucket)).To(Succeed())

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(cloudBucket),
		})
		Expect(err).NotTo(HaveOccurred())

		actual := &storagev1alpha1.CloudBucket{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), actual)).To(Succeed())
		Expect(actual.Finalizers).To(ContainElement(cloudBucketFinalizer))
		Expect(actual.Status.ObservedGeneration).To(Equal(actual.Generation))
		Expect(actual.Status.ActualBucketName).To(Equal(cloudBucket.Spec.BucketName))
		Expect(actual.Status.Endpoint).To(Equal("fake://cloudbucket.local"))
		Expect(actual.Status.Provider).To(Equal(defaultProvider))
		Expect(actual.Status.Region).To(Equal(defaultRegion))

		ready := meta.FindStatusCondition(actual.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal(reasonBucketProvisioned))

		bucketProvisioned := meta.FindStatusCondition(actual.Status.Conditions, conditionBucketProvisioned)
		Expect(bucketProvisioned).NotTo(BeNil())
		Expect(bucketProvisioned.Status).To(Equal(metav1.ConditionTrue))
		Expect(bucketProvisioned.Reason).To(Equal(reasonBucketProvisioned))
	})

	It("keeps successful reconciliation idempotent", func() {
		cloudBucket := newCloudBucket("idempotent-bucket", "idempotent-bucket-creds")
		Expect(k8sClient.Create(ctx, credentialsSecret(cloudBucket.Spec.CredentialsSecretName))).To(Succeed())
		Expect(k8sClient.Create(ctx, cloudBucket)).To(Succeed())

		request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cloudBucket)}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		first := &storagev1alpha1.CloudBucket{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), first)).To(Succeed())
		firstStatus := first.Status.DeepCopy()

		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		second := &storagev1alpha1.CloudBucket{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), second)).To(Succeed())
		Expect(second.Status).To(Equal(*firstStatus))
		Expect(second.Finalizers).To(Equal(first.Finalizers))
	})

	It("removes the finalizer after a simulated bucket delete", func() {
		cloudBucket := newCloudBucket("delete-bucket", "delete-bucket-creds")
		Expect(k8sClient.Create(ctx, credentialsSecret(cloudBucket.Spec.CredentialsSecretName))).To(Succeed())
		Expect(k8sClient.Create(ctx, cloudBucket)).To(Succeed())

		request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cloudBucket)}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		actual := &storagev1alpha1.CloudBucket{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), actual)).To(Succeed())
		Expect(actual.Finalizers).To(ContainElement(cloudBucketFinalizer))

		Expect(k8sClient.Delete(ctx, actual)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), &storagev1alpha1.CloudBucket{})
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())
	})

	It("keeps the finalizer when the credentials Secret is missing during delete", func() {
		cloudBucket := newCloudBucket("delete-missing-secret-bucket", "delete-missing-secret-creds")
		secret := credentialsSecret(cloudBucket.Spec.CredentialsSecretName)
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		Expect(k8sClient.Create(ctx, cloudBucket)).To(Succeed())

		request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cloudBucket)}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		actual := &storagev1alpha1.CloudBucket{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), actual)).To(Succeed())
		Expect(k8sClient.Delete(ctx, actual)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		terminating := &storagev1alpha1.CloudBucket{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cloudBucket), terminating)).To(Succeed())
		Expect(terminating.Finalizers).To(ContainElement(cloudBucketFinalizer))
		Expect(terminating.DeletionTimestamp.IsZero()).To(BeFalse())

		ready := meta.FindStatusCondition(terminating.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(reasonCredentialsMissing))
	})
})

func newCloudBucket(name string, secretName string) *storagev1alpha1.CloudBucket {
	return &storagev1alpha1.CloudBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: storagev1alpha1.CloudBucketSpec{
			BucketName:            name,
			CredentialsSecretName: secretName,
		},
	}
}

func credentialsSecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		StringData: map[string]string{
			"endpoint":  "fake://cloudbucket.local",
			"accessKey": "fake-access-key",
			"secretKey": "fake-secret-key",
			"useSSL":    "false",
		},
	}
}

func cleanupCloudBuckets(ctx context.Context, namespace string) {
	list := &storagev1alpha1.CloudBucketList{}
	Expect(k8sClient.List(ctx, list, client.InNamespace(namespace))).To(Succeed())
	for i := range list.Items {
		cloudBucket := &list.Items[i]
		key := types.NamespacedName{Name: cloudBucket.Name, Namespace: cloudBucket.Namespace}

		current := &storagev1alpha1.CloudBucket{}
		if err := k8sClient.Get(ctx, key, current); apierrors.IsNotFound(err) {
			continue
		} else {
			Expect(err).NotTo(HaveOccurred())
		}

		if len(current.Finalizers) > 0 {
			current.Finalizers = nil
			Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, current))).To(Succeed())
	}
}

func cleanupSecrets(ctx context.Context, namespace string) {
	list := &corev1.SecretList{}
	Expect(k8sClient.List(ctx, list, client.InNamespace(namespace))).To(Succeed())
	for i := range list.Items {
		secret := &list.Items[i]
		if secret.Type == corev1.SecretTypeServiceAccountToken {
			continue
		}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, secret))).To(Succeed())
	}
}
