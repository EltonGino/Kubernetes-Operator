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
	"strings"
	"testing"

	storagev1alpha1 "github.com/EltonGino/Kubernetes-Operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestCloudBucketDefault(t *testing.T) {
	cloudBucket := &storagev1alpha1.CloudBucket{
		Spec: storagev1alpha1.CloudBucketSpec{
			BucketName: "demo-bucket",
		},
	}

	defaulter := &CloudBucketCustomDefaulter{}
	if err := defaulter.Default(context.Background(), cloudBucket); err != nil {
		t.Fatalf("Default() returned error: %v", err)
	}

	if cloudBucket.Spec.Provider != defaultProvider {
		t.Fatalf("provider = %q, want %q", cloudBucket.Spec.Provider, defaultProvider)
	}
	if cloudBucket.Spec.Region != defaultRegion {
		t.Fatalf("region = %q, want %q", cloudBucket.Spec.Region, defaultRegion)
	}
	if cloudBucket.Spec.CredentialsSecretName != defaultCredentialsSecretName {
		t.Fatalf(
			"credentialsSecretName = %q, want %q",
			cloudBucket.Spec.CredentialsSecretName,
			defaultCredentialsSecretName,
		)
	}
}

func TestCloudBucketValidateCreate(t *testing.T) {
	tests := []struct {
		name          string
		spec          storagev1alpha1.CloudBucketSpec
		wantErr       bool
		wantSubstring string
	}{
		{
			name: "valid minio bucket",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "demo-bucket-123",
				Provider:   providerMinIO,
				Region:     defaultRegion,
			},
		},
		{
			name: "valid with defaults",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "demo-bucket-123",
			},
		},
		{
			name:          "missing bucket name",
			spec:          storagev1alpha1.CloudBucketSpec{},
			wantErr:       true,
			wantSubstring: "bucketName is required",
		},
		{
			name: "bucket name too short",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "ab",
			},
			wantErr:       true,
			wantSubstring: "at least 3 characters",
		},
		{
			name: "bucket name too long",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: strings.Repeat("a", 64),
			},
			wantErr:       true,
			wantSubstring: "at most 63 characters",
		},
		{
			name: "bucket name uppercase",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "Demo-bucket",
			},
			wantErr:       true,
			wantSubstring: "must be lowercase",
		},
		{
			name: "bucket name underscore",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "demo_bucket",
			},
			wantErr:       true,
			wantSubstring: "must not contain underscores",
		},
		{
			name: "bucket name starts with hyphen",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "-demo-bucket",
			},
			wantErr:       true,
			wantSubstring: "must start and end with a lowercase letter or number",
		},
		{
			name: "bucket name ends with hyphen",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "demo-bucket-",
			},
			wantErr:       true,
			wantSubstring: "must start and end with a lowercase letter or number",
		},
		{
			name: "unsupported provider",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "demo-bucket",
				Provider:   "aws",
			},
			wantErr:       true,
			wantSubstring: "Unsupported value: \"aws\"",
		},
		{
			name: "empty region",
			spec: storagev1alpha1.CloudBucketSpec{
				BucketName: "demo-bucket",
				Region:     "   ",
			},
			wantErr:       true,
			wantSubstring: "region must not be empty",
		},
	}

	validator := &CloudBucketCustomValidator{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloudBucket := &storagev1alpha1.CloudBucket{Spec: tt.spec}
			_, err := validator.ValidateCreate(context.Background(), cloudBucket)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidateCreate() returned error: %v", err)
				}
				return
			}

			assertInvalidError(t, err, tt.wantSubstring)
		})
	}
}

func TestCloudBucketValidateUpdate(t *testing.T) {
	validator := &CloudBucketCustomValidator{}
	oldCloudBucket := &storagev1alpha1.CloudBucket{
		Spec: storagev1alpha1.CloudBucketSpec{BucketName: "old-bucket"},
	}
	newCloudBucket := &storagev1alpha1.CloudBucket{
		Spec: storagev1alpha1.CloudBucketSpec{BucketName: "Invalid_Bucket"},
	}

	_, err := validator.ValidateUpdate(context.Background(), oldCloudBucket, newCloudBucket)
	assertInvalidError(t, err, "bucketName must be lowercase")
}

func TestCloudBucketValidateDelete(t *testing.T) {
	validator := &CloudBucketCustomValidator{}
	cloudBucket := &storagev1alpha1.CloudBucket{}

	if _, err := validator.ValidateDelete(context.Background(), cloudBucket); err != nil {
		t.Fatalf("ValidateDelete() returned error: %v", err)
	}
}

func assertInvalidError(t *testing.T, err error, wantSubstring string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if !apierrors.IsInvalid(err) {
		t.Fatalf("expected invalid Kubernetes API error, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), wantSubstring) {
		t.Fatalf("error %q does not contain %q", err.Error(), wantSubstring)
	}
}
