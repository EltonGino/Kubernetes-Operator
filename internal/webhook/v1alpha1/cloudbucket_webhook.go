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

package v1alpha1

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	storagev1alpha1 "github.com/EltonGino/Kubernetes-Operator/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var cloudbucketlog = logf.Log.WithName("cloudbucket-resource")

const (
	defaultProvider              = "minio"
	defaultRegion                = "us-south"
	defaultCredentialsSecretName = "cloudbucket-credentials"

	providerMinIO = "minio"
	providerIBM   = "ibm"
)

var bucketNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// SetupCloudBucketWebhookWithManager registers the webhook for CloudBucket in the manager.
func SetupCloudBucketWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&storagev1alpha1.CloudBucket{}).
		WithValidator(&CloudBucketCustomValidator{}).
		WithDefaulter(&CloudBucketCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-storage-example-com-v1alpha1-cloudbucket,mutating=true,failurePolicy=fail,sideEffects=None,groups=storage.example.com,resources=cloudbuckets,verbs=create;update,versions=v1alpha1,name=mcloudbucket-v1alpha1.kb.io,admissionReviewVersions=v1

// CloudBucketCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind CloudBucket when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
// +kubebuilder:object:generate=false
type CloudBucketCustomDefaulter struct {
}

var _ webhook.CustomDefaulter = &CloudBucketCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind CloudBucket.
func (d *CloudBucketCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	cloudbucket, ok := obj.(*storagev1alpha1.CloudBucket)

	if !ok {
		return fmt.Errorf("expected a CloudBucket object but got %T", obj)
	}
	cloudbucketlog.Info("Defaulting for CloudBucket", "name", cloudbucket.GetName())

	applyCloudBucketDefaults(&cloudbucket.Spec)

	return nil
}

// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-storage-example-com-v1alpha1-cloudbucket,mutating=false,failurePolicy=fail,sideEffects=None,groups=storage.example.com,resources=cloudbuckets,verbs=create;update,versions=v1alpha1,name=vcloudbucket-v1alpha1.kb.io,admissionReviewVersions=v1

// CloudBucketCustomValidator struct is responsible for validating the CloudBucket resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
// +kubebuilder:object:generate=false
type CloudBucketCustomValidator struct {
}

var _ webhook.CustomValidator = &CloudBucketCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type CloudBucket.
func (v *CloudBucketCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cloudbucket, ok := obj.(*storagev1alpha1.CloudBucket)
	if !ok {
		return nil, fmt.Errorf("expected a CloudBucket object but got %T", obj)
	}
	cloudbucketlog.Info("Validation for CloudBucket upon creation", "name", cloudbucket.GetName())

	return nil, validateCloudBucket(cloudbucket)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type CloudBucket.
func (v *CloudBucketCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	cloudbucket, ok := newObj.(*storagev1alpha1.CloudBucket)
	if !ok {
		return nil, fmt.Errorf("expected a CloudBucket object for the newObj but got %T", newObj)
	}
	cloudbucketlog.Info("Validation for CloudBucket upon update", "name", cloudbucket.GetName())

	return nil, validateCloudBucket(cloudbucket)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type CloudBucket.
func (v *CloudBucketCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cloudbucket, ok := obj.(*storagev1alpha1.CloudBucket)
	if !ok {
		return nil, fmt.Errorf("expected a CloudBucket object but got %T", obj)
	}
	cloudbucketlog.Info("Validation for CloudBucket upon deletion", "name", cloudbucket.GetName())

	return nil, nil
}

func applyCloudBucketDefaults(spec *storagev1alpha1.CloudBucketSpec) {
	if spec.Provider == "" {
		spec.Provider = defaultProvider
	}
	if spec.Region == "" {
		spec.Region = defaultRegion
	}
	if spec.CredentialsSecretName == "" {
		spec.CredentialsSecretName = defaultCredentialsSecretName
	}
}

func validateCloudBucket(cloudBucket *storagev1alpha1.CloudBucket) error {
	spec := cloudBucket.Spec
	applyCloudBucketDefaults(&spec)

	allErrs := make(field.ErrorList, 0, 3)
	specPath := field.NewPath("spec")

	allErrs = append(allErrs, validateBucketName(spec.BucketName, specPath.Child("bucketName"))...)
	allErrs = append(allErrs, validateProvider(spec.Provider, specPath.Child("provider"))...)
	allErrs = append(allErrs, validateRegion(spec.Region, specPath.Child("region"))...)

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "storage.example.com", Kind: "CloudBucket"},
		cloudBucket.Name,
		allErrs,
	)
}

func validateBucketName(bucketName string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if bucketName == "" {
		return append(allErrs, field.Required(fldPath, "bucketName is required"))
	}

	if len(bucketName) < 3 {
		allErrs = append(allErrs, field.Invalid(fldPath, bucketName, "bucketName must be at least 3 characters"))
	}
	if len(bucketName) > 63 {
		allErrs = append(allErrs, field.Invalid(fldPath, bucketName, "bucketName must be at most 63 characters"))
	}
	if bucketName != strings.ToLower(bucketName) {
		allErrs = append(allErrs, field.Invalid(fldPath, bucketName, "bucketName must be lowercase"))
	}
	if strings.Contains(bucketName, "_") {
		allErrs = append(allErrs, field.Invalid(fldPath, bucketName, "bucketName must not contain underscores"))
	}
	if !bucketNamePattern.MatchString(bucketName) {
		allErrs = append(allErrs, field.Invalid(
			fldPath,
			bucketName,
			"bucketName may contain only lowercase letters, numbers, and hyphens, and must start and end with a lowercase letter or number",
		))
	}

	return allErrs
}

func validateProvider(provider string, fldPath *field.Path) field.ErrorList {
	switch provider {
	case providerMinIO, providerIBM:
		return nil
	default:
		return field.ErrorList{
			field.NotSupported(fldPath, provider, []string{providerMinIO, providerIBM}),
		}
	}
}

func validateRegion(region string, fldPath *field.Path) field.ErrorList {
	if strings.TrimSpace(region) == "" {
		return field.ErrorList{
			field.Required(fldPath, "region must not be empty"),
		}
	}

	return nil
}
