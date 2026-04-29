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
	"net/http"
	"strings"
	"testing"

	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/request"
	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	corev1 "k8s.io/api/core/v1"
)

func TestIBMCOSCredentialsFromSecret(t *testing.T) {
	secret := ibmCOSCredentialsSecret()

	credentials, err := IBMCOSCredentialsFromSecret(secret)
	if err != nil {
		t.Fatalf("IBMCOSCredentialsFromSecret returned error: %v", err)
	}

	if credentials.APIKey != "fake-api-key" {
		t.Fatalf("APIKey = %q, want fake-api-key", credentials.APIKey)
	}
	if credentials.ResourceInstanceID != "crn:v1:bluemix:public:cloud-object-storage:global:a/test::" {
		t.Fatalf("ResourceInstanceID = %q, want test CRN", credentials.ResourceInstanceID)
	}
	if credentials.Region != "us-south" {
		t.Fatalf("Region = %q, want us-south", credentials.Region)
	}
	if credentials.Endpoint != "https://s3.us-south.cloud-object-storage.appdomain.cloud" {
		t.Fatalf("Endpoint = %q, want IBM COS us-south endpoint", credentials.Endpoint)
	}
	if credentials.CRN != credentials.ResourceInstanceID {
		t.Fatalf("CRN = %q, want resource instance CRN", credentials.CRN)
	}
}

func TestIBMCOSCredentialsFromSecretRequiresExpectedKeys(t *testing.T) {
	_, err := IBMCOSCredentialsFromSecret(&corev1.Secret{
		Data: map[string][]byte{
			ibmAPIKeyKey: []byte("fake-api-key"),
			ibmRegionKey: []byte("us-south"),
		},
	})
	if err == nil {
		t.Fatal("IBMCOSCredentialsFromSecret returned nil error for missing resourceInstanceID")
	}
	if !strings.Contains(err.Error(), `missing required key "resourceInstanceID"`) {
		t.Fatalf("error = %q, want missing resourceInstanceID message", err.Error())
	}
}

func TestIBMCOSCredentialsFromSecretDoesNotExposeValues(t *testing.T) {
	_, err := IBMCOSCredentialsFromSecret(&corev1.Secret{
		Data: map[string][]byte{
			ibmAPIKeyKey:             []byte("very-sensitive-api-key"),
			ibmResourceInstanceIDKey: []byte("very-sensitive-resource-id"),
			ibmRegionKey:             []byte("INVALID_REGION"),
		},
	})
	if err == nil {
		t.Fatal("IBMCOSCredentialsFromSecret returned nil error for invalid values")
	}

	errorText := err.Error()
	for _, sensitive := range []string{
		"very-sensitive-api-key",
		"very-sensitive-resource-id",
		"INVALID_REGION",
	} {
		if strings.Contains(errorText, sensitive) {
			t.Fatalf("error %q exposed sensitive value %q", errorText, sensitive)
		}
	}
}

func TestNewServiceForProviderSelectsIBM(t *testing.T) {
	service, err := NewServiceForProvider(ProviderIBM, ibmCOSCredentialsSecret())
	if err != nil {
		t.Fatalf("NewServiceForProvider returned error: %v", err)
	}

	ibmService, ok := service.(*IBMCOSService)
	if !ok {
		t.Fatalf("service type = %T, want *IBMCOSService", service)
	}
	if ibmService.endpoint != "https://s3.us-south.cloud-object-storage.appdomain.cloud" {
		t.Fatalf("endpoint = %q, want IBM COS us-south endpoint", ibmService.endpoint)
	}
}

func TestIsSupportedProviderIncludesIBM(t *testing.T) {
	if !IsSupportedProvider(ProviderIBM) {
		t.Fatal("IsSupportedProvider(ProviderIBM) = false, want true")
	}
}

func TestIBMCOSServiceEnsureBucketTreatsAlreadyExistingBucketAsSuccess(t *testing.T) {
	client := &fakeIBMCOSClient{
		headErr:   awserr.New(s3.ErrCodeNoSuchBucket, "not found", nil),
		createErr: awserr.New(s3.ErrCodeBucketAlreadyOwnedByYou, "already owned", nil),
	}
	service := newTestIBMCOSService(client)

	info, err := service.EnsureBucket(context.Background(), "existing-bucket", "us-south")
	if err != nil {
		t.Fatalf("EnsureBucket returned error: %v", err)
	}
	if !client.createCalled {
		t.Fatal("EnsureBucket did not attempt to create missing bucket")
	}
	if info.Name != "existing-bucket" {
		t.Fatalf("BucketInfo.Name = %q, want existing-bucket", info.Name)
	}
}

func TestIBMCOSServiceDeleteBucketTreatsMissingBucketAsSuccess(t *testing.T) {
	client := &fakeIBMCOSClient{
		headErr: awserr.NewRequestFailure(awserr.New("NotFound", "not found", nil), http.StatusNotFound, ""),
	}
	service := newTestIBMCOSService(client)

	if err := service.DeleteBucket(context.Background(), "missing-bucket", "us-south"); err != nil {
		t.Fatalf("DeleteBucket returned error: %v", err)
	}
	if client.deleteCalled {
		t.Fatal("DeleteBucket called provider delete for an already-missing bucket")
	}
}

func ibmCOSCredentialsSecret() *corev1.Secret {
	return &corev1.Secret{
		Data: map[string][]byte{
			ibmAPIKeyKey:             []byte("fake-api-key"),
			ibmResourceInstanceIDKey: []byte("crn:v1:bluemix:public:cloud-object-storage:global:a/test::"),
			ibmRegionKey:             []byte("us-south"),
		},
	}
}

func newTestIBMCOSService(client *fakeIBMCOSClient) *IBMCOSService {
	return &IBMCOSService{
		client:   client,
		endpoint: ibmCOSEndpointForRegion("us-south"),
		crn:      "crn:v1:bluemix:public:cloud-object-storage:global:a/test::",
		region:   "us-south",
	}
}

type fakeIBMCOSClient struct {
	createErr error
	headErr   error
	deleteErr error

	createCalled bool
	deleteCalled bool
}

func (f *fakeIBMCOSClient) CreateBucketWithContext(
	context.Context,
	*s3.CreateBucketInput,
	...request.Option,
) (*s3.CreateBucketOutput, error) {
	f.createCalled = true
	return &s3.CreateBucketOutput{}, f.createErr
}

func (f *fakeIBMCOSClient) HeadBucketWithContext(
	context.Context,
	*s3.HeadBucketInput,
	...request.Option,
) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, f.headErr
}

func (f *fakeIBMCOSClient) DeleteBucketWithContext(
	context.Context,
	*s3.DeleteBucketInput,
	...request.Option,
) (*s3.DeleteBucketOutput, error) {
	f.deleteCalled = true
	return &s3.DeleteBucketOutput{}, f.deleteErr
}
