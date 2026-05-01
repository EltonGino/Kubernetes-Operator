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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CloudBucketSpec defines the desired state of CloudBucket.
type CloudBucketSpec struct {
	// BucketName is the desired object storage bucket name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	BucketName string `json:"bucketName"`

	// Region is the target storage region.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=us-south
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9-]+$`
	Region string `json:"region,omitempty"`

	// Provider selects the object storage backend.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=minio
	// +kubebuilder:validation:Enum=minio;ibm
	Provider string `json:"provider,omitempty"`

	// CredentialsSecretName is the Secret containing provider credentials.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=cloudbucket-credentials
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`
	CredentialsSecretName string `json:"credentialsSecretName,omitempty"`
}

// CloudBucketStatus defines the observed state of CloudBucket.
type CloudBucketStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the CloudBucket state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Endpoint is the object storage endpoint for the provisioned bucket.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// CRN is the IBM Cloud Resource Name for the bucket when using IBM Cloud Object Storage.
	// +optional
	CRN string `json:"crn,omitempty"`

	// ActualBucketName is the bucket name confirmed by the provider.
	// +optional
	ActualBucketName string `json:"actualBucketName,omitempty"`

	// Provider is the storage backend used by the controller.
	// +optional
	Provider string `json:"provider,omitempty"`

	// Region is the storage region used by the controller.
	// +optional
	Region string `json:"region,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.spec.bucketName`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Region",type=string,JSONPath=`.spec.region`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CloudBucket is the Schema for the cloudbuckets API.
type CloudBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudBucketSpec   `json:"spec,omitempty"`
	Status CloudBucketStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudBucketList contains a list of CloudBucket.
type CloudBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudBucket `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudBucket{}, &CloudBucketList{})
}
