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
	"errors"
	"fmt"
	"time"

	storagev1alpha1 "github.com/EltonGino/Kubernetes-Operator/api/v1alpha1"
	"github.com/EltonGino/Kubernetes-Operator/internal/bucket"
	cloudmetrics "github.com/EltonGino/Kubernetes-Operator/internal/metrics"
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
	defaultProvider              = bucket.ProviderMinIO
	defaultCredentialsSecretName = "cloudbucket-credentials"

	conditionReady                = "Ready"
	conditionCredentialsAvailable = "CredentialsAvailable"
	conditionBucketProvisioned    = "BucketProvisioned"

	reasonCredentialsMissing    = "CredentialsMissing"
	reasonCredentialsInvalid    = "CredentialsInvalid"
	reasonCredentialsFound      = "CredentialsFound"
	reasonBucketProvisioned     = "BucketProvisioned"
	reasonProviderUnsupported   = "ProviderUnsupported"
	reasonBucketProvisionFailed = "BucketProvisionFailed"
	reasonBucketDeleted         = "BucketDeleted"
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
func (r *CloudBucketReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	startedAt := time.Now()
	metricProvider := cloudmetrics.ProviderUnknown
	metricResult := cloudmetrics.ResultSuccess
	metricReason := cloudmetrics.ReasonUnknown
	markReconcileError := func(reason string) {
		metricResult = cloudmetrics.ResultError
		metricReason = reason
	}
	setMetricProvider := func(provider string) {
		metricProvider = provider
	}
	defer func() {
		if retErr != nil && metricResult != cloudmetrics.ResultError {
			markReconcileError(cloudmetrics.ReasonBucketError)
		}
		cloudmetrics.RecordReconcile(metricProvider, metricResult, time.Since(startedAt))
		if metricResult == cloudmetrics.ResultError {
			cloudmetrics.RecordReconcileError(metricProvider, metricReason)
		}
	}()

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
	setMetricProvider(provider)

	if !cloudBucket.DeletionTimestamp.IsZero() {
		if err := r.reconcileDelete(
			ctx,
			cloudBucket,
			secretName,
			provider,
			region,
			log,
			setMetricProvider,
			markReconcileError,
		); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !bucket.IsSupportedProvider(provider) {
		log.Info("provider is not supported", "provider", provider)
		if err := r.setProviderUnsupportedStatus(ctx, cloudBucket, provider, region); err != nil {
			markReconcileError(cloudmetrics.ReasonStatusUpdateError)
			return ctrl.Result{}, err
		}
		markReconcileError(cloudmetrics.ReasonProviderUnsupported)
		r.recordEvent(cloudBucket, corev1.EventTypeWarning, reasonProviderUnsupported,
			fmt.Sprintf("Provider %q is not supported yet", provider))
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(cloudBucket, cloudBucketFinalizer) {
		controllerutil.AddFinalizer(cloudBucket, cloudBucketFinalizer)
		if err := r.Update(ctx, cloudBucket); err != nil {
			cloudmetrics.RecordFinalizerOperation(
				provider,
				cloudmetrics.FinalizerOperationAdd,
				cloudmetrics.ResultError,
			)
			markReconcileError(cloudmetrics.ReasonFinalizerError)
			return ctrl.Result{}, err
		}
		cloudmetrics.RecordFinalizerOperation(
			provider,
			cloudmetrics.FinalizerOperationAdd,
			cloudmetrics.ResultSuccess,
		)
		log.Info("added finalizer")
	}

	secret, err := r.getCredentialsSecret(ctx, cloudBucket, secretName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cloudmetrics.RecordCredentialCheck(
				provider,
				cloudmetrics.ResultError,
				cloudmetrics.CredentialReasonMissing,
			)
			log.Info("credentials Secret is missing", "secret", secretName)
			if err := r.setCredentialsMissingStatus(ctx, cloudBucket, provider, region, secretName); err != nil {
				markReconcileError(cloudmetrics.ReasonStatusUpdateError)
				return ctrl.Result{}, err
			}
			markReconcileError(cloudmetrics.ReasonCredentialsMissing)
			r.recordEvent(cloudBucket, corev1.EventTypeWarning, reasonCredentialsMissing,
				fmt.Sprintf("Credentials Secret %q was not found", secretName))
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	bucketService, err := r.bucketService(provider, secret)
	if err != nil {
		serviceErr := err
		if isCredentialsInvalid(serviceErr) {
			cloudmetrics.RecordCredentialCheck(
				provider,
				cloudmetrics.ResultError,
				cloudmetrics.CredentialReasonInvalid,
			)
			log.Info("credentials Secret is invalid", "secret", secretName)
			if err := r.setCredentialsInvalidStatus(ctx, cloudBucket, provider, region, secretName, serviceErr); err != nil {
				markReconcileError(cloudmetrics.ReasonStatusUpdateError)
				return ctrl.Result{}, err
			}
			markReconcileError(cloudmetrics.ReasonCredentialsInvalid)
			r.recordEvent(cloudBucket, corev1.EventTypeWarning, reasonCredentialsInvalid,
				fmt.Sprintf("Credentials Secret %q is invalid", secretName))
			return ctrl.Result{}, nil
		}
		if isUnsupportedProvider(serviceErr) {
			if err := r.setProviderUnsupportedStatus(ctx, cloudBucket, provider, region); err != nil {
				markReconcileError(cloudmetrics.ReasonStatusUpdateError)
				return ctrl.Result{}, err
			}
			markReconcileError(cloudmetrics.ReasonProviderUnsupported)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	cloudmetrics.RecordCredentialCheck(
		provider,
		cloudmetrics.ResultSuccess,
		cloudmetrics.CredentialReasonFound,
	)

	bucketOperationStartedAt := time.Now()
	info, err := bucketService.EnsureBucket(ctx, cloudBucket.Spec.BucketName, region)
	bucketOperationResult := cloudmetrics.ResultSuccess
	if err != nil {
		bucketOperationResult = cloudmetrics.ResultError
	}
	cloudmetrics.RecordBucketOperation(
		provider,
		cloudmetrics.OperationEnsureBucket,
		bucketOperationResult,
		time.Since(bucketOperationStartedAt),
	)
	if err != nil {
		cloudmetrics.RecordBucketOperationError(
			provider,
			cloudmetrics.OperationEnsureBucket,
			cloudmetrics.ReasonBucketError,
		)
		if statusErr := r.setBucketProvisionFailedStatus(ctx, cloudBucket, provider, region); statusErr != nil {
			markReconcileError(cloudmetrics.ReasonStatusUpdateError)
			return ctrl.Result{}, statusErr
		}
		markReconcileError(cloudmetrics.ReasonBucketError)
		return ctrl.Result{}, err
	}

	if err := r.setProvisionedStatus(ctx, cloudBucket, provider, region, info); err != nil {
		markReconcileError(cloudmetrics.ReasonStatusUpdateError)
		return ctrl.Result{}, err
	}
	r.recordEvent(cloudBucket, corev1.EventTypeNormal, reasonBucketProvisioned,
		fmt.Sprintf("Bucket %q was provisioned", info.Name))
	log.Info("bucket reconciled", "bucket", info.Name, "provider", provider, "region", region)

	return ctrl.Result{}, nil
}

func (r *CloudBucketReconciler) reconcileDelete(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	secretName string,
	provider string,
	region string,
	log logr.Logger,
	setMetricProvider func(string),
	markReconcileError func(string),
) error {
	if !controllerutil.ContainsFinalizer(cloudBucket, cloudBucketFinalizer) {
		return nil
	}

	deleteProvider := provider
	if cloudBucket.Status.Provider != "" {
		deleteProvider = cloudBucket.Status.Provider
	}
	setMetricProvider(deleteProvider)

	secret, err := r.getCredentialsSecret(ctx, cloudBucket, secretName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cloudmetrics.RecordCredentialCheck(
				deleteProvider,
				cloudmetrics.ResultError,
				cloudmetrics.CredentialReasonMissing,
			)
			log.Info("credentials Secret is missing during delete", "secret", secretName)
			if err := r.setCredentialsMissingStatus(
				ctx,
				cloudBucket,
				deleteProvider,
				region,
				secretName,
			); err != nil {
				markReconcileError(cloudmetrics.ReasonStatusUpdateError)
				return err
			}
			markReconcileError(cloudmetrics.ReasonCredentialsMissing)
			return nil
		}
		return err
	}

	bucketService, err := r.bucketService(deleteProvider, secret)
	if err != nil {
		serviceErr := err
		if isCredentialsInvalid(serviceErr) {
			cloudmetrics.RecordCredentialCheck(
				deleteProvider,
				cloudmetrics.ResultError,
				cloudmetrics.CredentialReasonInvalid,
			)
			log.Info("credentials Secret is invalid during delete", "secret", secretName)
			if err := r.setCredentialsInvalidStatus(ctx, cloudBucket, deleteProvider, region, secretName, serviceErr); err != nil {
				markReconcileError(cloudmetrics.ReasonStatusUpdateError)
				return err
			}
			markReconcileError(cloudmetrics.ReasonCredentialsInvalid)
			return nil
		}
		if isUnsupportedProvider(serviceErr) {
			log.Info("provider is not supported during delete", "provider", deleteProvider)
			if err := r.setProviderUnsupportedStatus(ctx, cloudBucket, deleteProvider, region); err != nil {
				markReconcileError(cloudmetrics.ReasonStatusUpdateError)
				return err
			}
			markReconcileError(cloudmetrics.ReasonProviderUnsupported)
			return nil
		}
		return err
	}
	cloudmetrics.RecordCredentialCheck(
		deleteProvider,
		cloudmetrics.ResultSuccess,
		cloudmetrics.CredentialReasonFound,
	)

	bucketOperationStartedAt := time.Now()
	if err := bucketService.DeleteBucket(ctx, cloudBucket.Spec.BucketName, region); err != nil {
		cloudmetrics.RecordBucketOperation(
			deleteProvider,
			cloudmetrics.OperationDeleteBucket,
			cloudmetrics.ResultError,
			time.Since(bucketOperationStartedAt),
		)
		cloudmetrics.RecordBucketOperationError(
			deleteProvider,
			cloudmetrics.OperationDeleteBucket,
			cloudmetrics.ReasonBucketError,
		)
		markReconcileError(cloudmetrics.ReasonBucketError)
		return err
	}
	cloudmetrics.RecordBucketOperation(
		deleteProvider,
		cloudmetrics.OperationDeleteBucket,
		cloudmetrics.ResultSuccess,
		time.Since(bucketOperationStartedAt),
	)

	controllerutil.RemoveFinalizer(cloudBucket, cloudBucketFinalizer)
	if err := r.Update(ctx, cloudBucket); err != nil {
		cloudmetrics.RecordFinalizerOperation(
			deleteProvider,
			cloudmetrics.FinalizerOperationRemove,
			cloudmetrics.ResultError,
		)
		markReconcileError(cloudmetrics.ReasonFinalizerError)
		return err
	}
	cloudmetrics.RecordFinalizerOperation(
		deleteProvider,
		cloudmetrics.FinalizerOperationRemove,
		cloudmetrics.ResultSuccess,
	)

	r.recordEvent(cloudBucket, corev1.EventTypeNormal, reasonBucketDeleted,
		fmt.Sprintf("Bucket %q was deleted", cloudBucket.Spec.BucketName))
	log.Info("removed finalizer after bucket delete", "bucket", cloudBucket.Spec.BucketName)
	return nil
}

func (r *CloudBucketReconciler) getCredentialsSecret(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	secretName string,
) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: cloudBucket.Namespace,
	}, secret)
	return secret, err
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

func (r *CloudBucketReconciler) setCredentialsInvalidStatus(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	provider string,
	region string,
	secretName string,
	validationErr error,
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
			Reason:             reasonCredentialsInvalid,
			Message:            fmt.Sprintf("Credentials Secret %q is invalid: %s", secretName, validationErr.Error()),
		})
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonCredentialsInvalid,
			Message:            "Provider credentials are invalid",
		})
	})
}

func (r *CloudBucketReconciler) setProviderUnsupportedStatus(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	provider string,
	region string,
) error {
	return r.updateStatusIfChanged(ctx, cloudBucket, func() {
		cloudBucket.Status.ObservedGeneration = cloudBucket.Generation
		cloudBucket.Status.ActualBucketName = ""
		cloudBucket.Status.Endpoint = ""
		cloudBucket.Status.CRN = ""
		cloudBucket.Status.Provider = provider
		cloudBucket.Status.Region = region
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionBucketProvisioned,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonProviderUnsupported,
			Message:            "Bucket was not provisioned because the provider is unsupported",
		})
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonProviderUnsupported,
			Message:            fmt.Sprintf("Provider %q is not supported yet", provider),
		})
	})
}

func (r *CloudBucketReconciler) setBucketProvisionFailedStatus(
	ctx context.Context,
	cloudBucket *storagev1alpha1.CloudBucket,
	provider string,
	region string,
) error {
	return r.updateStatusIfChanged(ctx, cloudBucket, func() {
		cloudBucket.Status.ObservedGeneration = cloudBucket.Generation
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
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonBucketProvisionFailed,
			Message:            "Bucket could not be provisioned",
		})
		meta.SetStatusCondition(&cloudBucket.Status.Conditions, metav1.Condition{
			Type:               conditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cloudBucket.Generation,
			Reason:             reasonBucketProvisionFailed,
			Message:            "Bucket is not ready",
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
	actualRegion := region
	if info.Region != "" {
		actualRegion = info.Region
	}

	return r.updateStatusIfChanged(ctx, cloudBucket, func() {
		cloudBucket.Status.ObservedGeneration = cloudBucket.Generation
		cloudBucket.Status.ActualBucketName = info.Name
		cloudBucket.Status.Endpoint = info.Endpoint
		cloudBucket.Status.CRN = info.CRN
		cloudBucket.Status.Provider = provider
		cloudBucket.Status.Region = actualRegion
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
			Message:            "Bucket was provisioned",
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

func (r *CloudBucketReconciler) bucketService(provider string, secret *corev1.Secret) (bucket.Service, error) {
	if r.BucketService != nil {
		return r.BucketService, nil
	}
	return bucket.NewServiceForProvider(provider, secret)
}

func isCredentialsInvalid(err error) bool {
	var invalidCredentials bucket.InvalidCredentialsError
	return errors.As(err, &invalidCredentials)
}

func isUnsupportedProvider(err error) bool {
	var unsupportedProvider bucket.UnsupportedProviderError
	return errors.As(err, &unsupportedProvider)
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
