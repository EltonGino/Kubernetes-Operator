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
	"strconv"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	corev1 "k8s.io/api/core/v1"
)

const (
	minIOEndpointKey  = "endpoint"
	minIOAccessKeyKey = "accessKey"
	minIOSecretKeyKey = "secretKey"
	minIOUseSSLKey    = "useSSL"
)

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

// MinIOCredentials contains the Secret values needed to connect to MinIO.
type MinIOCredentials struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// MinIOService provisions S3-compatible buckets in a MinIO server.
type MinIOService struct {
	client   *minio.Client
	endpoint string
}

// NewMinIOServiceFromSecret builds a MinIO bucket service from Kubernetes Secret data.
func NewMinIOServiceFromSecret(secret *corev1.Secret) (*MinIOService, error) {
	minioCredentials, err := MinIOCredentialsFromSecret(secret)
	if err != nil {
		return nil, err
	}

	client, err := minio.New(minioCredentials.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioCredentials.AccessKey, minioCredentials.SecretKey, ""),
		Secure: minioCredentials.UseSSL,
	})
	if err != nil {
		return nil, InvalidCredentialsError{
			Provider: ProviderMinIO,
			Problems: []string{
				"endpoint must be a host[:port] value without a URL scheme or path",
			},
		}
	}

	return &MinIOService{
		client:   client,
		endpoint: minioCredentials.Endpoint,
	}, nil
}

// MinIOCredentialsFromSecret validates and extracts MinIO credentials from a Secret.
func MinIOCredentialsFromSecret(secret *corev1.Secret) (MinIOCredentials, error) {
	if secret == nil {
		return MinIOCredentials{}, InvalidCredentialsError{
			Provider: ProviderMinIO,
			Problems: []string{
				"credentials Secret is nil",
			},
		}
	}

	endpoint, endpointOK := secretValue(secret, minIOEndpointKey)
	accessKey, accessKeyOK := secretValue(secret, minIOAccessKeyKey)
	secretKey, secretKeyOK := secretValue(secret, minIOSecretKeyKey)
	useSSLText, useSSLOK := secretValue(secret, minIOUseSSLKey)

	var problems []string
	if !endpointOK {
		problems = append(problems, fmt.Sprintf("missing required key %q", minIOEndpointKey))
	}
	if endpointOK && strings.Contains(endpoint, "://") {
		problems = append(problems, fmt.Sprintf("key %q must not include a URL scheme", minIOEndpointKey))
	}
	if endpointOK && strings.Contains(endpoint, "/") {
		problems = append(problems, fmt.Sprintf("key %q must not include a URL path", minIOEndpointKey))
	}
	if !accessKeyOK {
		problems = append(problems, fmt.Sprintf("missing required key %q", minIOAccessKeyKey))
	}
	if !secretKeyOK {
		problems = append(problems, fmt.Sprintf("missing required key %q", minIOSecretKeyKey))
	}
	if !useSSLOK {
		problems = append(problems, fmt.Sprintf("missing required key %q", minIOUseSSLKey))
	}

	useSSL := false
	if useSSLOK {
		parsed, err := strconv.ParseBool(useSSLText)
		if err != nil {
			problems = append(problems, fmt.Sprintf("key %q must be a boolean", minIOUseSSLKey))
		} else {
			useSSL = parsed
		}
	}

	if len(problems) > 0 {
		return MinIOCredentials{}, InvalidCredentialsError{
			Provider: ProviderMinIO,
			Problems: problems,
		}
	}

	return MinIOCredentials{
		Endpoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
		UseSSL:    useSSL,
	}, nil
}

// EnsureBucket creates the bucket if needed and treats existing buckets as success.
func (s *MinIOService) EnsureBucket(ctx context.Context, bucketName string, region string) (*BucketInfo, error) {
	exists, err := s.BucketExists(ctx, bucketName, region)
	if err != nil {
		return nil, err
	}

	if !exists {
		if err := s.client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: region}); err != nil {
			if !isMinIOErrorCode(err, "BucketAlreadyOwnedByYou", "BucketAlreadyExists") {
				return nil, err
			}
		}
	}

	return &BucketInfo{
		Name:     bucketName,
		Endpoint: s.endpoint,
		Region:   region,
	}, nil
}

// BucketExists checks whether the bucket exists in MinIO.
func (s *MinIOService) BucketExists(ctx context.Context, bucketName string, _ string) (bool, error) {
	exists, err := s.client.BucketExists(ctx, bucketName)
	if err != nil && isMinIOErrorCode(err, "NoSuchBucket") {
		return false, nil
	}
	return exists, err
}

// DeleteBucket removes the bucket and treats missing buckets as success.
func (s *MinIOService) DeleteBucket(ctx context.Context, bucketName string, region string) error {
	exists, err := s.BucketExists(ctx, bucketName, region)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if err := s.client.RemoveBucket(ctx, bucketName); err != nil {
		if isMinIOErrorCode(err, "NoSuchBucket") {
			return nil
		}
		return err
	}
	return nil
}

func secretValue(secret *corev1.Secret, key string) (string, bool) {
	if value, ok := secret.Data[key]; ok {
		text := strings.TrimSpace(string(value))
		return text, text != ""
	}
	if value, ok := secret.StringData[key]; ok {
		text := strings.TrimSpace(value)
		return text, text != ""
	}
	return "", false
}

func isMinIOErrorCode(err error, codes ...string) bool {
	response := minio.ToErrorResponse(err)
	if response.Code == "" {
		return false
	}
	for _, code := range codes {
		if response.Code == code {
			return true
		}
	}
	return false
}
