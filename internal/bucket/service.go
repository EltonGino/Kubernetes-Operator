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

package bucket

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// ProviderMinIO is the local S3-compatible provider used for development.
	ProviderMinIO = "minio"
	// ProviderIBM is IBM Cloud Object Storage.
	ProviderIBM = "ibm"
)

// BucketInfo describes a bucket confirmed by a storage provider.
type BucketInfo struct {
	Name     string
	Endpoint string
	CRN      string
	Region   string
}

// Service hides provider-specific object storage behavior from the controller.
type Service interface {
	EnsureBucket(ctx context.Context, bucketName string, region string) (*BucketInfo, error)
	DeleteBucket(ctx context.Context, bucketName string, region string) error
	BucketExists(ctx context.Context, bucketName string, region string) (bool, error)
}

// UnsupportedProviderError reports a provider that is not implemented yet.
type UnsupportedProviderError struct {
	Provider string
}

func (e UnsupportedProviderError) Error() string {
	return fmt.Sprintf("unsupported bucket provider %q", e.Provider)
}

// InvalidCredentialsError describes invalid Secret data without exposing values.
type InvalidCredentialsError struct {
	Provider string
	Problems []string
}

func (e InvalidCredentialsError) Error() string {
	if len(e.Problems) == 0 {
		return fmt.Sprintf("invalid %s credentials", e.Provider)
	}
	return fmt.Sprintf("invalid %s credentials: %s", e.Provider, strings.Join(e.Problems, "; "))
}

// NewServiceForProvider creates the storage provider implementation selected by the CR.
func NewServiceForProvider(provider string, secret *corev1.Secret) (Service, error) {
	switch provider {
	case ProviderMinIO:
		return NewMinIOServiceFromSecret(secret)
	case ProviderIBM:
		return NewIBMCOSServiceFromSecret(secret)
	default:
		return nil, UnsupportedProviderError{Provider: provider}
	}
}

// IsSupportedProvider reports whether this phase can reconcile the provider.
func IsSupportedProvider(provider string) bool {
	return provider == ProviderMinIO || provider == ProviderIBM
}
