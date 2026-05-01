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
	"context"
	"errors"
	"sync"
	"time"

	storagev1alpha1 "github.com/EltonGino/Kubernetes-Operator/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	ProviderMinIO   = "minio"
	ProviderIBM     = "ibm"
	ProviderUnknown = "unknown"

	ResultSuccess = "success"
	ResultError   = "error"

	OperationReconcile    = "reconcile"
	OperationEnsureBucket = "ensure_bucket"
	OperationDeleteBucket = "delete_bucket"

	FinalizerOperationAdd    = "add"
	FinalizerOperationRemove = "remove"

	ReasonCredentialsMissing  = "credentials_missing"
	ReasonCredentialsInvalid  = "credentials_invalid"
	ReasonProviderUnsupported = "provider_unsupported"
	ReasonBucketError         = "bucket_error"
	ReasonStatusUpdateError   = "status_update_error"
	ReasonFinalizerError      = "finalizer_error"
	ReasonUnknown             = "unknown"

	CredentialReasonFound   = "found"
	CredentialReasonMissing = "missing"
	CredentialReasonInvalid = "invalid"
)

var (
	providerLabelValues = []string{ProviderMinIO, ProviderIBM, ProviderUnknown}
	resultLabelValues   = []string{ResultSuccess, ResultError}
	reasonLabelValues   = []string{
		ReasonCredentialsMissing,
		ReasonCredentialsInvalid,
		ReasonProviderUnsupported,
		ReasonBucketError,
		ReasonStatusUpdateError,
		ReasonFinalizerError,
		ReasonUnknown,
	}
	bucketOperationLabelValues    = []string{OperationReconcile, OperationEnsureBucket, OperationDeleteBucket}
	finalizerOperationLabelValues = []string{FinalizerOperationAdd, FinalizerOperationRemove}
	credentialReasonLabelValues   = []string{CredentialReasonFound, CredentialReasonMissing, CredentialReasonInvalid}

	reconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbucket_reconcile_total",
			Help: "Total number of CloudBucket reconcile attempts grouped by provider and result.",
		},
		[]string{"provider", "result"},
	)
	reconcileErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbucket_reconcile_errors_total",
			Help: "Total number of CloudBucket reconcile failures grouped by provider and safe reason.",
		},
		[]string{"provider", "reason"},
	)
	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cloudbucket_reconcile_duration_seconds",
			Help:    "Duration of CloudBucket reconcile loops grouped by provider and result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provider", "result"},
	)

	bucketOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbucket_bucket_operations_total",
			Help: "Total number of CloudBucket provider bucket operations grouped by provider, operation, and result.",
		},
		[]string{"provider", "operation", "result"},
	)
	bucketOperationErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbucket_bucket_operation_errors_total",
			Help: "Total number of CloudBucket provider bucket operation failures grouped by provider, operation, and safe reason.",
		},
		[]string{"provider", "operation", "reason"},
	)
	bucketOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cloudbucket_bucket_operation_duration_seconds",
			Help:    "Duration of CloudBucket provider bucket operations grouped by provider, operation, and result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provider", "operation", "result"},
	)

	finalizerOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbucket_finalizer_operations_total",
			Help: "Total number of CloudBucket finalizer operations grouped by provider, operation, and result.",
		},
		[]string{"provider", "operation", "result"},
	)

	credentialsChecksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cloudbucket_credentials_checks_total",
			Help: "Total number of CloudBucket credential checks grouped by provider, result, and safe reason.",
		},
		[]string{"provider", "result", "reason"},
	)

	stateCollectorOnce sync.Once
	stateCollectorErr  error
)

func init() {
	if err := registerCollectors(
		ctrlmetrics.Registry,
		reconcileTotal,
		reconcileErrorsTotal,
		reconcileDuration,
		bucketOperationsTotal,
		bucketOperationErrorsTotal,
		bucketOperationDuration,
		finalizerOperationsTotal,
		credentialsChecksTotal,
	); err != nil {
		panic(err)
	}

	initializeMetricSeries()
}

// RegisterCloudBucketStateCollector registers aggregate Ready/NotReady gauges.
// The collector lists CloudBucket resources at scrape time, which avoids
// per-resource labels and keeps state metrics from drifting between reconciles.
func RegisterCloudBucketStateCollector(reader client.Reader) error {
	if reader == nil {
		return errors.New("cloudbucket state metrics reader is nil")
	}

	stateCollectorOnce.Do(func() {
		stateCollectorErr = registerCollectors(ctrlmetrics.Registry, newCloudBucketStateCollector(reader))
	})

	return stateCollectorErr
}

// RecordReconcile records a reconcile attempt and its duration.
func RecordReconcile(provider, result string, duration time.Duration) {
	provider = ProviderLabel(provider)
	result = ResultLabel(result)

	reconcileTotal.WithLabelValues(provider, result).Inc()
	reconcileDuration.WithLabelValues(provider, result).Observe(duration.Seconds())
}

// RecordReconcileError records a reconcile error reason.
func RecordReconcileError(provider, reason string) {
	reconcileErrorsTotal.WithLabelValues(ProviderLabel(provider), ReasonLabel(reason)).Inc()
}

// RecordBucketOperation records a provider bucket operation and its duration.
func RecordBucketOperation(provider, operation, result string, duration time.Duration) {
	provider = ProviderLabel(provider)
	operation = BucketOperationLabel(operation)
	result = ResultLabel(result)

	bucketOperationsTotal.WithLabelValues(provider, operation, result).Inc()
	bucketOperationDuration.WithLabelValues(provider, operation, result).Observe(duration.Seconds())
}

// RecordBucketOperationError records a provider bucket operation error reason.
func RecordBucketOperationError(provider, operation, reason string) {
	bucketOperationErrorsTotal.WithLabelValues(
		ProviderLabel(provider),
		BucketOperationLabel(operation),
		ReasonLabel(reason),
	).Inc()
}

// RecordFinalizerOperation records a finalizer add or remove operation.
func RecordFinalizerOperation(provider, operation, result string) {
	finalizerOperationsTotal.WithLabelValues(
		ProviderLabel(provider),
		FinalizerOperationLabel(operation),
		ResultLabel(result),
	).Inc()
}

// RecordCredentialCheck records the outcome of reading and validating provider credentials.
func RecordCredentialCheck(provider, result, reason string) {
	credentialsChecksTotal.WithLabelValues(
		ProviderLabel(provider),
		ResultLabel(result),
		CredentialReasonLabel(reason),
	).Inc()
}

// ProviderLabel returns a controlled provider label value.
func ProviderLabel(provider string) string {
	switch provider {
	case ProviderMinIO, ProviderIBM:
		return provider
	default:
		return ProviderUnknown
	}
}

// ResultLabel returns a controlled result label value.
func ResultLabel(result string) string {
	switch result {
	case ResultSuccess, ResultError:
		return result
	default:
		return ResultError
	}
}

// ReasonLabel returns a controlled error reason label value.
func ReasonLabel(reason string) string {
	switch reason {
	case ReasonCredentialsMissing, "CredentialsMissing":
		return ReasonCredentialsMissing
	case ReasonCredentialsInvalid, "CredentialsInvalid":
		return ReasonCredentialsInvalid
	case ReasonProviderUnsupported, "ProviderUnsupported":
		return ReasonProviderUnsupported
	case ReasonBucketError, "BucketProvisionFailed":
		return ReasonBucketError
	case ReasonStatusUpdateError:
		return ReasonStatusUpdateError
	case ReasonFinalizerError:
		return ReasonFinalizerError
	default:
		return ReasonUnknown
	}
}

// BucketOperationLabel returns a controlled bucket operation label value.
func BucketOperationLabel(operation string) string {
	switch operation {
	case OperationReconcile, OperationEnsureBucket, OperationDeleteBucket:
		return operation
	default:
		return OperationReconcile
	}
}

// FinalizerOperationLabel returns a controlled finalizer operation label value.
func FinalizerOperationLabel(operation string) string {
	switch operation {
	case FinalizerOperationAdd, FinalizerOperationRemove:
		return operation
	default:
		return FinalizerOperationAdd
	}
}

// CredentialReasonLabel returns a controlled credential reason label value.
func CredentialReasonLabel(reason string) string {
	switch reason {
	case CredentialReasonFound, CredentialReasonMissing, CredentialReasonInvalid:
		return reason
	default:
		return CredentialReasonInvalid
	}
}

func registerCollectors(registerer prometheus.Registerer, collectors ...prometheus.Collector) error {
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			var alreadyRegistered prometheus.AlreadyRegisteredError
			if errors.As(err, &alreadyRegistered) {
				continue
			}

			return err
		}
	}

	return nil
}

type cloudBucketStateCollector struct {
	reader       client.Reader
	readyDesc    *prometheus.Desc
	notReadyDesc *prometheus.Desc
}

type cloudBucketStateSnapshot struct {
	ready    map[string]int
	notReady map[stateKey]int
}

type stateKey struct {
	provider string
	reason   string
}

func newCloudBucketStateCollector(reader client.Reader) prometheus.Collector {
	return &cloudBucketStateCollector{
		reader: reader,
		readyDesc: prometheus.NewDesc(
			"cloudbucket_ready",
			"Number of CloudBucket resources currently Ready=True grouped by provider.",
			[]string{"provider"},
			nil,
		),
		notReadyDesc: prometheus.NewDesc(
			"cloudbucket_not_ready",
			"Number of CloudBucket resources currently not Ready=True grouped by provider and safe reason.",
			[]string{"provider", "reason"},
			nil,
		),
	}
}

func (c *cloudBucketStateCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.readyDesc
	ch <- c.notReadyDesc
}

func (c *cloudBucketStateCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cloudBuckets := &storagev1alpha1.CloudBucketList{}
	if err := c.reader.List(ctx, cloudBuckets); err != nil {
		return
	}

	snapshot := aggregateCloudBucketStates(cloudBuckets.Items)
	for provider, count := range snapshot.ready {
		ch <- prometheus.MustNewConstMetric(c.readyDesc, prometheus.GaugeValue, float64(count), provider)
	}
	for key, count := range snapshot.notReady {
		ch <- prometheus.MustNewConstMetric(c.notReadyDesc, prometheus.GaugeValue, float64(count), key.provider, key.reason)
	}
}

func aggregateCloudBucketStates(cloudBuckets []storagev1alpha1.CloudBucket) cloudBucketStateSnapshot {
	snapshot := cloudBucketStateSnapshot{
		ready:    make(map[string]int),
		notReady: make(map[stateKey]int),
	}

	for _, provider := range providerLabelValues {
		snapshot.ready[provider] = 0
		for _, reason := range reasonLabelValues {
			snapshot.notReady[stateKey{provider: provider, reason: reason}] = 0
		}
	}

	for _, cloudBucket := range cloudBuckets {
		provider := cloudBucketStateProvider(cloudBucket)
		readyCondition := apiMeta.FindStatusCondition(cloudBucket.Status.Conditions, "Ready")
		if readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
			snapshot.ready[provider]++
			continue
		}

		reason := ReasonUnknown
		if readyCondition != nil {
			reason = ReasonLabel(readyCondition.Reason)
		}
		snapshot.notReady[stateKey{provider: provider, reason: reason}]++
	}

	return snapshot
}

func cloudBucketStateProvider(cloudBucket storagev1alpha1.CloudBucket) string {
	if cloudBucket.Status.Provider != "" {
		return ProviderLabel(cloudBucket.Status.Provider)
	}
	if cloudBucket.Spec.Provider != "" {
		return ProviderLabel(cloudBucket.Spec.Provider)
	}

	return ProviderMinIO
}

func initializeMetricSeries() {
	for _, provider := range providerLabelValues {
		for _, result := range resultLabelValues {
			reconcileTotal.WithLabelValues(provider, result)
			reconcileDuration.WithLabelValues(provider, result)
		}

		for _, reason := range reasonLabelValues {
			reconcileErrorsTotal.WithLabelValues(provider, reason)
		}

		for _, operation := range bucketOperationLabelValues {
			for _, result := range resultLabelValues {
				bucketOperationsTotal.WithLabelValues(provider, operation, result)
				bucketOperationDuration.WithLabelValues(provider, operation, result)
			}
			for _, reason := range reasonLabelValues {
				bucketOperationErrorsTotal.WithLabelValues(provider, operation, reason)
			}
		}

		for _, operation := range finalizerOperationLabelValues {
			for _, result := range resultLabelValues {
				finalizerOperationsTotal.WithLabelValues(provider, operation, result)
			}
		}

		for _, result := range resultLabelValues {
			for _, reason := range credentialReasonLabelValues {
				credentialsChecksTotal.WithLabelValues(provider, result, reason)
			}
		}
	}
}
