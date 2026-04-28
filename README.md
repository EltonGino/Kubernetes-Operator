# IBM CloudBucket Operator

[![Tests](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/test.yml/badge.svg)](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/test.yml)
[![E2E Tests](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/test-e2e.yml/badge.svg)](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/test-e2e.yml)
[![Lint](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/lint.yml/badge.svg)](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/lint.yml)

IBM CloudBucket Operator is a production-style Kubernetes Operator written in Go. It introduces a `CloudBucket` custom resource and reconciles that desired state into a real object storage bucket.

The current implementation provisions buckets in MinIO for local development. IBM Cloud Object Storage support is planned as the next provider phase.

## What The Operator Does

The operator watches `CloudBucket` resources in Kubernetes and makes object storage match the requested spec.

Example:

```yaml
apiVersion: storage.example.com/v1alpha1
kind: CloudBucket
metadata:
  name: cloudbucket-sample
spec:
  bucketName: elton-demo-bucket-123
  provider: minio
  region: us-south
  credentialsSecretName: cloudbucket-credentials
```

When this resource is applied, the controller:

- Adds a finalizer so external bucket cleanup can happen before Kubernetes removes the resource.
- Reads provider credentials from a Kubernetes Secret in the same namespace.
- Creates the bucket in MinIO if it does not already exist.
- Treats an already-existing bucket as a successful, idempotent result.
- Updates `.status` with provider, region, endpoint, actual bucket name, observed generation, and conditions.
- Deletes the bucket during resource deletion and then removes the finalizer.

## Architecture Overview

```text
CloudBucket CR
    |
    v
CloudBucketReconciler
    |
    +-- Kubernetes API
    |     +-- CloudBucket status
    |     +-- Finalizers
    |     +-- Events
    |     +-- Secret lookup
    |
    v
BucketService interface
    |
    +-- MinIOService
    +-- IBM COS provider planned
```

The controller is responsible for Kubernetes behavior: watching resources, managing finalizers, reading Secrets, updating status, and recording events.

Provider-specific storage behavior lives behind the `BucketService` abstraction under `internal/bucket`, which keeps the reconciler small and makes future providers easier to add.

## Reconciliation Flow

On create or update:

1. Fetch the `CloudBucket`.
2. Resolve defaults for provider, region, and Secret name.
3. Add the finalizer if it is missing.
4. Read the credentials Secret from the same namespace.
5. Build the provider-specific bucket service.
6. Ensure the bucket exists.
7. Update status fields and conditions.
8. Record a Kubernetes event.

On delete:

1. Detect the deletion timestamp.
2. Keep the resource present while the finalizer exists.
3. Read the credentials Secret.
4. Delete the external bucket.
5. Treat a missing bucket as successful cleanup.
6. Remove the finalizer so Kubernetes can finish deleting the resource.

The reconcile loop is designed to be idempotent: repeated reconciles should converge on the same bucket and status without creating duplicate external resources.

## Tech Stack

- Go
- Operator SDK
- Kubebuilder and controller-runtime
- Kubernetes CustomResourceDefinitions
- Kubernetes finalizers, status conditions, RBAC, Events, and Secrets
- MinIO for local S3-compatible object storage
- kind for local Kubernetes testing
- Docker for local containers and image builds
- GitHub Actions for tests, linting, and e2e verification

## Current Features

- `CloudBucket` CRD in group `storage.example.com/v1alpha1`
- Required `spec.bucketName`
- Defaulted `spec.provider: minio`
- Defaulted `spec.region: us-south`
- Defaulted `spec.credentialsSecretName: cloudbucket-credentials`
- Status subresource with useful fields and conditions
- Printer columns for bucket, provider, region, readiness, and age
- MinIO bucket provisioning
- MinIO bucket deletion through finalizers
- Missing and invalid credentials handling
- Least-privilege Secret access using `get` only
- Kubernetes Events for important lifecycle transitions
- Unit tests and e2e tests in CI

## Key Kubernetes Concepts

### CRD

A CustomResourceDefinition extends the Kubernetes API. This project adds:

```text
cloudbuckets.storage.example.com
```

After the CRD is installed, users can manage object storage buckets with normal `kubectl` workflows.

### Controller Reconciliation

The controller compares the desired state in `spec` with the real world. If the bucket is missing, it creates it. If the resource is deleted, it cleans up the bucket before allowing Kubernetes to remove the object.

### Finalizers

The finalizer `storage.example.com/cloudbucket-finalizer` prevents Kubernetes from immediately deleting a `CloudBucket`. This gives the operator a chance to delete the external bucket first, then remove the finalizer.

### Status Conditions

The operator writes Kubernetes-style conditions so users and automation can understand the current state:

- `CredentialsAvailable`
- `BucketProvisioned`
- `Ready`

Failures such as missing Secrets or invalid credentials are reported as `Ready=False` with a reason and message.

### Credentials Via Kubernetes Secret

MinIO credentials are stored in a Secret instead of the custom resource:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cloudbucket-credentials
type: Opaque
stringData:
  endpoint: "localhost:9000"
  accessKey: "minioadmin"
  secretKey: "minioadmin"
  useSSL: "false"
```

The operator validates required Secret keys but does not log Secret values.

### Least-Privilege RBAC

The generated controller role grants the reconciler only the access it needs:

- Manage `cloudbuckets` and their status/finalizers.
- Read Secrets with `get`.
- Create and patch Events.

The controller does not need broad Secret permissions such as `list`, `watch`, `create`, `update`, or `delete`.

## Local Demo With MinIO

These commands run the current working flow locally. They assume Docker is running and `kubectl` points to a working Kubernetes cluster such as kind.

On macOS with Homebrew GNU Make, use `gmake`. On Linux, the same targets usually work with `make`.

### 1. Start MinIO

```sh
docker rm -f minio 2>/dev/null || true

docker run -d --name minio \
  -p 9000:9000 \
  -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin \
  -e MINIO_ROOT_PASSWORD=minioadmin \
  quay.io/minio/minio server /data --console-address ":9001"
```

MinIO API:

```text
http://localhost:9000
```

MinIO console:

```text
http://localhost:9001
```

### 2. Install The CRD

```sh
gmake install
```

### 3. Apply The MinIO Secret

```sh
kubectl apply -f config/samples/storage_v1alpha1_minio_secret.yaml
```

The sample Secret is for local development only.

### 4. Run The Operator Locally

Run this in a separate terminal and keep it running:

```sh
go run ./cmd/main.go
```

### 5. Apply A CloudBucket

```sh
kubectl apply -f config/samples/storage_v1alpha1_cloudbucket.yaml
```

### 6. Check Status

```sh
kubectl get cloudbucket

kubectl wait \
  --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
  cloudbucket/cloudbucket-sample \
  --timeout=60s

kubectl get cloudbucket cloudbucket-sample -o yaml
```

Expected high-level result:

```text
NAME                 BUCKET                  PROVIDER   REGION     READY
cloudbucket-sample   elton-demo-bucket-123   minio      us-south   True
```

### 7. Verify The Bucket In MinIO

```sh
docker run --rm --network container:minio --entrypoint /bin/sh quay.io/minio/mc -c \
  'mc alias set local http://127.0.0.1:9000 minioadmin minioadmin >/dev/null && mc ls local'
```

Expected result includes:

```text
elton-demo-bucket-123/
```

### 8. Delete The CloudBucket And Confirm Cleanup

```sh
kubectl delete -f config/samples/storage_v1alpha1_cloudbucket.yaml

kubectl get cloudbucket

docker run --rm --network container:minio --entrypoint /bin/sh quay.io/minio/mc -c \
  'mc alias set local http://127.0.0.1:9000 minioadmin minioadmin >/dev/null && mc ls local'
```

After deletion, Kubernetes should show no `CloudBucket` resources in the namespace, and the MinIO bucket listing should no longer include `elton-demo-bucket-123/`.

## Development Commands

```sh
go fmt ./...
go mod tidy
gmake generate
gmake manifests
go test ./...
git diff --check
```

Build the manager image:

```sh
gmake docker-build IMG=example.com/kubernetes-operator:v0.0.1
```

Run e2e tests:

```sh
gmake test-e2e
```

## CI Status

GitHub Actions currently verifies:

- Unit and controller tests
- E2E tests against a kind cluster
- golangci-lint

The workflows are intended to catch scaffold drift, lint regressions, Docker build issues, and reconciliation behavior problems before changes are merged.

## Roadmap

- IBM Cloud Object Storage provider
- Controller metrics
- Validation webhook
- OLM/OpenShift bundle
- GitHub Actions polish
- Architecture diagram

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

```text
http://www.apache.org/licenses/LICENSE-2.0
```

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
