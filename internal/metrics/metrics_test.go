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

package metrics

import (
	"testing"

	storagev1alpha1 "github.com/EltonGino/Kubernetes-Operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProviderLabel(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     string
	}{
		{name: "minio", provider: ProviderMinIO, want: ProviderMinIO},
		{name: "ibm", provider: ProviderIBM, want: ProviderIBM},
		{name: "empty", provider: "", want: ProviderUnknown},
		{name: "unsupported", provider: "aws", want: ProviderUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProviderLabel(tt.provider); got != tt.want {
				t.Fatalf("ProviderLabel(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestReasonLabel(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{name: "credentials missing constant", reason: ReasonCredentialsMissing, want: ReasonCredentialsMissing},
		{name: "credentials missing condition", reason: "CredentialsMissing", want: ReasonCredentialsMissing},
		{name: "credentials invalid condition", reason: "CredentialsInvalid", want: ReasonCredentialsInvalid},
		{name: "provider unsupported condition", reason: "ProviderUnsupported", want: ReasonProviderUnsupported},
		{name: "bucket failure condition", reason: "BucketProvisionFailed", want: ReasonBucketError},
		{name: "unknown reason", reason: "SomethingElse", want: ReasonUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReasonLabel(tt.reason); got != tt.want {
				t.Fatalf("ReasonLabel(%q) = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}

func TestAggregateCloudBucketStates(t *testing.T) {
	cloudBuckets := []storagev1alpha1.CloudBucket{
		{
			Spec: storagev1alpha1.CloudBucketSpec{Provider: ProviderMinIO},
			Status: storagev1alpha1.CloudBucketStatus{
				Provider: ProviderMinIO,
				Conditions: []metav1.Condition{
					{Type: "Ready", Status: metav1.ConditionTrue, Reason: "BucketProvisioned"},
				},
			},
		},
		{
			Spec: storagev1alpha1.CloudBucketSpec{Provider: ProviderIBM},
			Status: storagev1alpha1.CloudBucketStatus{
				Provider: ProviderIBM,
				Conditions: []metav1.Condition{
					{Type: "Ready", Status: metav1.ConditionFalse, Reason: "CredentialsMissing"},
				},
			},
		},
		{
			Spec: storagev1alpha1.CloudBucketSpec{Provider: "aws"},
		},
		{
			Spec: storagev1alpha1.CloudBucketSpec{},
			Status: storagev1alpha1.CloudBucketStatus{
				Conditions: []metav1.Condition{
					{Type: "Ready", Status: metav1.ConditionTrue, Reason: "BucketProvisioned"},
				},
			},
		},
	}

	snapshot := aggregateCloudBucketStates(cloudBuckets)

	if got := snapshot.ready[ProviderMinIO]; got != 2 {
		t.Fatalf("ready[%q] = %d, want 2", ProviderMinIO, got)
	}

	ibmMissing := stateKey{provider: ProviderIBM, reason: ReasonCredentialsMissing}
	if got := snapshot.notReady[ibmMissing]; got != 1 {
		t.Fatalf("notReady[%#v] = %d, want 1", ibmMissing, got)
	}

	unknownReason := stateKey{provider: ProviderUnknown, reason: ReasonUnknown}
	if got := snapshot.notReady[unknownReason]; got != 1 {
		t.Fatalf("notReady[%#v] = %d, want 1", unknownReason, got)
	}
}
