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

import "context"

const fakeEndpoint = "fake://cloudbucket.local"

// FakeService lets the controller prove Kubernetes reconciliation behavior before
// any real object storage provider is introduced.
type FakeService struct{}

// NewFakeService returns a no-op bucket service for early controller development.
func NewFakeService() *FakeService {
	return &FakeService{}
}

// EnsureBucket pretends the requested bucket exists and returns stable provider data.
func (s *FakeService) EnsureBucket(_ context.Context, bucketName string, region string) (*BucketInfo, error) {
	return &BucketInfo{
		Name:     bucketName,
		Endpoint: fakeEndpoint,
		Region:   region,
	}, nil
}

// DeleteBucket pretends the requested bucket was deleted.
func (s *FakeService) DeleteBucket(_ context.Context, _ string, _ string) error {
	return nil
}

// BucketExists pretends the requested bucket already exists.
func (s *FakeService) BucketExists(_ context.Context, _ string, _ string) (bool, error) {
	return true, nil
}
