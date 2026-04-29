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
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam"
	"github.com/IBM/ibm-cos-sdk-go/aws/request"
	"github.com/IBM/ibm-cos-sdk-go/aws/session"
	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	corev1 "k8s.io/api/core/v1"
)

const (
	ibmAPIKeyKey             = "apiKey"
	ibmResourceInstanceIDKey = "resourceInstanceID"
	ibmRegionKey             = "region"

	ibmIAMAuthEndpoint = "https://iam.cloud.ibm.com/identity/token"
)

type ibmCOSClient interface {
	CreateBucketWithContext(context.Context, *s3.CreateBucketInput, ...request.Option) (*s3.CreateBucketOutput, error)
	HeadBucketWithContext(context.Context, *s3.HeadBucketInput, ...request.Option) (*s3.HeadBucketOutput, error)
	DeleteBucketWithContext(context.Context, *s3.DeleteBucketInput, ...request.Option) (*s3.DeleteBucketOutput, error)
}

// IBMCOSCredentials contains the Secret values needed to connect to IBM COS.
type IBMCOSCredentials struct {
	APIKey             string
	ResourceInstanceID string
	Region             string
	Endpoint           string
	CRN                string
}

// IBMCOSService provisions buckets in IBM Cloud Object Storage.
type IBMCOSService struct {
	client   ibmCOSClient
	endpoint string
	crn      string
	region   string
}

// NewIBMCOSServiceFromSecret builds an IBM COS bucket service from Kubernetes Secret data.
func NewIBMCOSServiceFromSecret(secret *corev1.Secret) (*IBMCOSService, error) {
	ibmCredentials, err := IBMCOSCredentialsFromSecret(secret)
	if err != nil {
		return nil, err
	}

	conf := aws.NewConfig().
		WithRegion(ibmCredentials.Region).
		WithEndpoint(ibmCredentials.Endpoint).
		WithCredentials(ibmiam.NewStaticCredentials(
			aws.NewConfig(),
			ibmIAMAuthEndpoint,
			ibmCredentials.APIKey,
			ibmCredentials.ResourceInstanceID,
		)).
		WithS3ForcePathStyle(true)

	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	return &IBMCOSService{
		client:   s3.New(sess, conf),
		endpoint: ibmCredentials.Endpoint,
		crn:      ibmCredentials.CRN,
		region:   ibmCredentials.Region,
	}, nil
}

// IBMCOSCredentialsFromSecret validates and extracts IBM COS credentials from a Secret.
func IBMCOSCredentialsFromSecret(secret *corev1.Secret) (IBMCOSCredentials, error) {
	if secret == nil {
		return IBMCOSCredentials{}, InvalidCredentialsError{
			Provider: ProviderIBM,
			Problems: []string{
				"credentials Secret is nil",
			},
		}
	}

	apiKey, apiKeyOK := secretValue(secret, ibmAPIKeyKey)
	resourceInstanceID, resourceInstanceIDOK := secretValue(secret, ibmResourceInstanceIDKey)
	region, regionOK := secretValue(secret, ibmRegionKey)

	var problems []string
	if !apiKeyOK {
		problems = append(problems, fmt.Sprintf("missing required key %q", ibmAPIKeyKey))
	}
	if !resourceInstanceIDOK {
		problems = append(problems, fmt.Sprintf("missing required key %q", ibmResourceInstanceIDKey))
	}
	if !regionOK {
		problems = append(problems, fmt.Sprintf("missing required key %q", ibmRegionKey))
	}
	if regionOK && !isIBMRegion(region) {
		problems = append(problems, fmt.Sprintf("key %q must contain only lowercase letters, numbers, and hyphens", ibmRegionKey))
	}

	if len(problems) > 0 {
		return IBMCOSCredentials{}, InvalidCredentialsError{
			Provider: ProviderIBM,
			Problems: problems,
		}
	}

	return IBMCOSCredentials{
		APIKey:             apiKey,
		ResourceInstanceID: resourceInstanceID,
		Region:             region,
		Endpoint:           ibmCOSEndpointForRegion(region),
		CRN:                crnFromResourceInstanceID(resourceInstanceID),
	}, nil
}

// EnsureBucket creates the bucket if needed and treats existing buckets as success.
func (s *IBMCOSService) EnsureBucket(ctx context.Context, bucketName string, region string) (*BucketInfo, error) {
	exists, err := s.BucketExists(ctx, bucketName, region)
	if err != nil {
		return nil, err
	}

	if !exists {
		input := &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		}
		if _, err := s.client.CreateBucketWithContext(ctx, input); err != nil {
			if !isIBMCOSAlreadyExistsError(err) {
				return nil, err
			}
		}
	}

	return &BucketInfo{
		Name:     bucketName,
		Endpoint: s.endpoint,
		CRN:      s.crn,
		Region:   s.region,
	}, nil
}

// BucketExists checks whether the bucket exists in IBM COS.
func (s *IBMCOSService) BucketExists(ctx context.Context, bucketName string, _ string) (bool, error) {
	_, err := s.client.HeadBucketWithContext(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil {
		return true, nil
	}
	if isIBMCOSNotFoundError(err) {
		return false, nil
	}
	return false, err
}

// DeleteBucket removes the bucket and treats missing buckets as success.
func (s *IBMCOSService) DeleteBucket(ctx context.Context, bucketName string, region string) error {
	exists, err := s.BucketExists(ctx, bucketName, region)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if _, err := s.client.DeleteBucketWithContext(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	}); err != nil {
		if isIBMCOSNotFoundError(err) {
			return nil
		}
		return err
	}
	return nil
}

func ibmCOSEndpointForRegion(region string) string {
	return fmt.Sprintf("https://s3.%s.cloud-object-storage.appdomain.cloud", region)
}

func crnFromResourceInstanceID(resourceInstanceID string) string {
	if strings.HasPrefix(resourceInstanceID, "crn:") {
		return resourceInstanceID
	}
	return ""
}

func isIBMRegion(region string) bool {
	if region == "" {
		return false
	}
	for _, r := range region {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			continue
		}
		return false
	}
	return true
}

func isIBMCOSAlreadyExistsError(err error) bool {
	return isIBMCOSAWSErrorCode(err, s3.ErrCodeBucketAlreadyExists, s3.ErrCodeBucketAlreadyOwnedByYou)
}

func isIBMCOSNotFoundError(err error) bool {
	return isIBMCOSAWSErrorCode(err, s3.ErrCodeNoSuchBucket, "NotFound", "404") ||
		isIBMCOSStatusCode(err, http.StatusNotFound)
}

func isIBMCOSAWSErrorCode(err error, codes ...string) bool {
	var awsErr awserr.Error
	if !errors.As(err, &awsErr) {
		return false
	}
	for _, code := range codes {
		if awsErr.Code() == code {
			return true
		}
	}
	return false
}

func isIBMCOSStatusCode(err error, statusCode int) bool {
	var requestFailure awserr.RequestFailure
	return errors.As(err, &requestFailure) && requestFailure.StatusCode() == statusCode
}
