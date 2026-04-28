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

	storagev1alpha1 "github.com/EltonGino/Kubernetes-Operator/api/v1alpha1"
	"github.com/EltonGino/Kubernetes-Operator/internal/bucket"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	cloudBucketFinalizer = "storage.example.com/cloudbucket-finalizer"

	defaultRegion                = "us-south"
	defaultProvider              = "minio"
	defaultCredentialsSecretName = "cloudbucket-credentials"

	conditionReady                = "Ready"
	conditionCredentialsAvailable = "CredentialsAvailable"
	conditionBucketProvisioned    = "BucketProvisioned"

	reasonCredentialsMissing = "CredentialsMissing"
	reasonCredentialsFound   = "CredentialsFound"
	reasonBucketProvisioned  = "BucketProvisioned"
)

// CloudBucketReconciler reconciles a CloudBucket object
type CloudBucketReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	BucketService bucket.Service
}

// +kubebuilder:rbac:groups=storage.example.com,resources=cloudbuckets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.example.com,resources=cloudbuckets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.example.com,resources=cloudbuckets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *CloudBucketReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("cloudbucket", req.NamespacedName)

	cloudBucket := &storagev1alpha1.CloudBucket{}
	if err := r.Get(ctx, req.NamespacedName, cloudBucket); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	region := cloudBucketRegion(cloudBucket)
	provider := cloudBucketProvider(cloudBucket)
	secretName := cloudBucketCredentialsSecretName(cloudBucket)

	if !cloudBucket.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cloudBucket, secretName, region, log)
	}

	if !controllerutil.ContainsFinalizer(cloudBucket, cloudBucketFinalizer) {
		controllerutil.AddFinalizer(cloudBucket, cloudBucketFinalizer)
		if err := r.Update(ctx, cloudBucket); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("added finalizer")
	}

	if err := r.getCredentialsSecret(ctx, cloudBucket, secretName); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("credentials Secret is missing", "secret", secretName)
			if err := r.setCredentialsMissingStatus(ctx, cloudBucket, provider, region, secretName); err != nil {
				return ctrl.Result{}, err
			}
			r.recordEvent(cloudBucket, corev1.EventTypeWarning, reasonCredentialsMissing,
				fmt.Sprintf("Credentials Secret %q was not found", secretName))
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	info, err := r.bucketService().EnsureBucket(ctx, cloudBucket.Spec.BucketName, region)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.setProvisionedStatus(ctx, cloudBucket, provider, region, info); err != nil {
		return ctrl.Result{}, err
	}
	r.recordEvent(cloudBucket, corev1.EventTypeNormal, reasonBucketProvisioned,
		fmt.Sprintf("Bucket %q was provisioned by the fake bucket service", info.Name))
	log.Info("bucket reconciled", "bucket", info.Name, "provider", provider, "region", region)

	return ctrl.Result{}, nil
}

func (r *CloudBucketReconciler) reconcileDelete(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	secretName string,
	region string,
	log logr.Logger,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(cloudBucket, cloudBucketFinalizer) {
		return ctrl.Result{}, nil
	}

	if err := r.getCredentialsSecret(ctx, cloudBucket, secretName); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("credentials Secret is missing during delete", "secret", secretName)
			if err := r.setCredentialsMissingStatus(
				ctx,
				cloudBucket,
				cloudBucketProvider(cloudBucket),
				region,
				secretName,
			); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if err := r.bucketService().DeleteBucket(ctx, cloudBucket.Spec.BucketName, region); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(cloudBucket, cloudBucketFinalizer)
	if err := r.Update(ctx, cloudBucket); err != nil {
		return ctrl.Result{}, err
	}

	r.recordEvent(cloudBucket, corev1.EventTypeNormal, "BucketDeleted",
		fmt.Sprintf("Bucket %q was deleted by the fake bucket service", cloudBucket.Spec.BucketName))
	log.Info("removed finalizer after simulated bucket delete", "bucket", cloudBucket.Spec.BucketName)
	return ctrl.Result{}, nil
}

func (r *CloudBucketReconciler) getCredentialsSecret(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	secretName string,
) error {
	secret := &corev1.Secret{}
	return r.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: cloudBucket.Namespace,
	}, secret)
}

func (r *CloudBucketReconciler) setCredentialsMissingStatus(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	provider string,
	region string,
	secretName string,
) error {
	return r.updateStatusIfChanged(ctx, cloudBucket, func() {
		cloudBucket.Status.ObservedGeneration = cloudBucket.Generation
		cloudBucket.Status.ActualBucketName = ""
		cloudBucket.Status.Endpoint = ""
		cloudBucket.Status.CRN = ""
		cloudBucket.Status.Provider = provider
		cloudBucket.Status.Region = region
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionCredentialsAvailable,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonCredentialsMissing,
			Message:            fmt.Sprintf("Credentials Secret %q was not found", secretName),
		})
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonCredentialsMissing,
			Message:            "Waiting for provider credentials",
		})
	})
}

func (r *CloudBucketReconciler) setProvisionedStatus(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	provider string,
	region string,
	info *bucket.BucketInfo,
) error {
	return r.updateStatusIfChanged(ctx, cloudBucket, func() {
		cloudBucket.Status.ObservedGeneration = cloudBucket.Generation
		cloudBucket.Status.ActualBucketName = info.Name
		cloudBucket.Status.Endpoint = info.Endpoint
		cloudBucket.Status.CRN = info.CRN
		cloudBucket.Status.Provider = provider
		cloudBucket.Status.Region = region
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionCredentialsAvailable,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonCredentialsFound,
			Message:            "Credentials Secret is available",
		})
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionBucketProvisioned,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonBucketProvisioned,
			Message:            "Bucket was provisioned by the fake bucket service",
		})
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonBucketProvisioned,
			Message:            "Bucket is ready",
		})
	})
}

func (r *CloudBucketReconciler) updateStatusIfChanged(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	mutate func(),
) error {
	before := cloudBucket.Status.DeepCopy()
	mutate()
	if apiequality.Semantic.DeepEqual(before, &cloudBucket.Status) {
		return nil
	}
	return r.Status().Update(ctx, cloudBucket)
}

func (r *CloudBucketReconciler) bucketService() bucket.Service {
	if r.BucketService != nil {
		return r.BucketService
	}
	return bucket.NewFakeService()
}

func (r *CloudBucketReconciler) recordEvent(
	cloudBucket *storagev1alpha1.CloudBucket,
	eventType string,
	reason string,
	message string,
) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Event(cloudBucket, eventType, reason, message)
}

func cloudBucketRegion(cloudBucket *storagev1alpha1.CloudBucket) string {
	if cloudBucket.Spec.Region == "" {
		return defaultRegion
	}
	return cloudBucket.Spec.Region
}

func cloudBucketProvider(cloudBucket *storagev1alpha1.CloudBucket) string {
	if cloudBucket.Spec.Provider == "" {
		return defaultProvider
	}
	return cloudBucket.Spec.Provider
}

func cloudBucketCredentialsSecretName(cloudBucket *storagev1alpha1.CloudBucket) string {
	if cloudBucket.Spec.CredentialsSecretName == "" {
		return defaultCredentialsSecretName
	}
	return cloudBucket.Spec.CredentialsSecretName
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudBucketReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.CloudBucket{}).
		Named("cloudbucket").
		Complete(r)
}
