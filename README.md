# IBM CloudBucket Operator

[![Tests](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/test.yml/badge.svg)](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/test.yml)
[![E2E Tests](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/test-e2e.yml/badge.svg)](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/test-e2e.yml)
[![Lint](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/lint.yml/badge.svg)](https://github.com/EltonGino/Kubernetes-Operator/actions/workflows/lint.yml)

IBM CloudBucket Operator is a production-style Kubernetes Operator written in Go. It adds a `CloudBucket` custom resource to Kubernetes and reconciles that desired state into an object storage bucket.

The project is designed to be fully evaluated without IBM Cloud, paid resources, or credit card verification. The primary demo path uses MinIO, a free S3-compatible object store that runs locally in Docker.

IBM Cloud Object Storage support is included as an optional enterprise integration path for users who already have IBM Cloud credentials. Live IBM COS testing requires user-provided IBM Cloud credentials and may require IBM account or credit card verification.

## Provider Support

| Provider | Status | Cost to evaluate | Notes |
| --- | --- | --- | --- |
| MinIO | Primary, fully tested local demo | Zero cost | Demonstrates the complete Kubernetes Operator behavior locally. |
| IBM Cloud Object Storage | Optional enterprise integration | Requires user-provided IBM Cloud account and credentials | Provider code and validation exist, but live testing depends on IBM Cloud access and account verification. |

MinIO is not a toy fallback. It is the main proof path for this project because it exercises the real operator mechanics:

- CRD installation
- Reconciliation loop
- Kubernetes Secret handling
- Provider abstraction
- Object storage bucket creation
- Status condition updates
- Finalizer-based cleanup
- Bucket deletion

## What The Operator Does

The operator watches `CloudBucket` resources and makes object storage match the requested Kubernetes spec.

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
- Creates the bucket if it does not already exist.
- Treats an already-existing bucket as a successful, idempotent result.
- Updates `.status` with provider, region, endpoint, actual bucket name, observed generation, and conditions.
- Deletes the bucket during resource deletion and then removes the finalizer.

## Architecture Overview

```text
CloudBucket custom resource
    |
    v
Kubernetes API server
    |
    v
CloudBucketReconciler
    |
    +-- Finalizer management
    +-- Secret lookup
    +-- Status conditions
    +-- Kubernetes Events
    |
    v
BucketService interface
    |
    +-- MinIOService       fully tested local provider
    +-- IBMCOSService      optional enterprise provider
```

The controller is responsible for Kubernetes behavior: watching resources, managing finalizers, reading Secrets, updating status, and recording Events.

Provider-specific storage behavior lives behind the `BucketService` abstraction under `internal/bucket`. This keeps the reconciler focused on Kubernetes orchestration and makes provider integrations replaceable.

## Reconciliation Flow

On create or update:

1. Fetch the `CloudBucket`.
2. Resolve defaults for provider, region, and Secret name.
3. Add the finalizer if it is missing.
4. Read the credentials Secret from the same namespace.
5. Build the selected provider service.
6. Ensure the bucket exists.
7. Update status fields and conditions.
8. Record a Kubernetes Event.

On delete:

1. Detect the deletion timestamp.
2. Keep the resource present while the finalizer exists.
3. Read the credentials Secret.
4. Delete the external bucket.
5. Treat a missing bucket as successful cleanup.
6. Remove the finalizer so Kubernetes can finish deleting the resource.

The reconcile loop is idempotent: repeated reconciles converge on the same bucket and status without creating duplicate external resources.

## Tech Stack

- Go
- Operator SDK
- Kubebuilder and controller-runtime
- Kubernetes CustomResourceDefinitions
- Kubernetes finalizers, status conditions, RBAC, Events, and Secrets
- MinIO for zero-cost local object storage
- IBM Cloud Object Storage SDK for optional enterprise integration
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
- Fully working MinIO bucket provisioning
- Fully working MinIO bucket deletion through finalizers
- Optional IBM COS provider implementation
- Missing and invalid credentials handling
- Least-privilege Secret access using `get` only
- Kubernetes Events for lifecycle transitions
- Unit, controller, lint, and e2e tests in CI

## Key Kubernetes Concepts Demonstrated

### CRD

A CustomResourceDefinition extends the Kubernetes API. This project adds:

```text
cloudbuckets.storage.example.com
```

After the CRD is installed, users can manage object storage buckets with normal `kubectl` workflows.

### Controller Reconciliation

The controller compares the desired state in `spec` with the real world. If the bucket is missing, it creates it. If the resource is deleted, it cleans up the bucket before allowing Kubernetes to remove the object.

The complete reconciliation behavior is demonstrable locally with MinIO.

### Finalizers

The finalizer `storage.example.com/cloudbucket-finalizer` prevents Kubernetes from immediately deleting a `CloudBucket`. This gives the operator a chance to delete the external bucket first, then remove the finalizer.

This cleanup flow works in the local MinIO demo.

### Status Conditions

The operator writes Kubernetes-style conditions so users and automation can understand the current state:

- `CredentialsAvailable`
- `BucketProvisioned`
- `Ready`

Failures such as missing Secrets or invalid credentials are reported as `Ready=False` with a reason and message.

### Credentials Via Kubernetes Secret

Credentials are stored in Kubernetes Secrets instead of the custom resource.

MinIO local demo Secret:

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

IBM COS optional Secret shape:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cloudbucket-ibm-credentials
type: Opaque
stringData:
  apiKey: "REPLACE_WITH_IBM_CLOUD_API_KEY"
  resourceInstanceID: "REPLACE_WITH_COS_RESOURCE_INSTANCE_CRN"
  region: "us-south"
```

Never include or commit real IBM Cloud credentials. The sample IBM manifest contains placeholders only.

### Least-Privilege RBAC

The generated controller role grants the reconciler only the access it needs:

- Manage `cloudbuckets` and their status/finalizers.
- Read Secrets with `get`.
- Create and patch Events.

The controller does not need broad Secret permissions such as `list`, `watch`, `create`, `update`, or `delete`.

## Local Demo With MinIO

This is the recommended way to evaluate the project.

The MinIO demo requires only:

- Go
- Docker or another Docker-compatible runtime
- kubectl
- kind or another Kubernetes cluster
- GNU Make as `gmake` on macOS, or `make` on Linux

It does not require IBM Cloud, an IBM account, or a credit card.

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

The full YAML status shows the operator behavior:

```yaml
status:
  actualBucketName: elton-demo-bucket-123
  endpoint: localhost:9000
  provider: minio
  region: us-south
  conditions:
    - type: CredentialsAvailable
      status: "True"
    - type: BucketProvisioned
      status: "True"
    - type: Ready
      status: "True"
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

This proves finalizer cleanup and external resource deletion.

## Optional IBM COS Testing

IBM COS support is included for users who have IBM Cloud access, but it is not required to evaluate the operator.

Live IBM COS testing requires:

- An IBM Cloud account
- IBM Cloud Object Storage instance
- IBM Cloud API key
- COS resource instance CRN
- A globally unique bucket name
- IBM account verification, which may require credit card verification

The sample files are:

```text
config/samples/storage_v1alpha1_ibm_secret.yaml
config/samples/storage_v1alpha1_cloudbucket_ibm.yaml
```

Before applying them to a real cluster, replace all placeholder values. Do not commit real credentials.

IBM COS status note:

The current IBM SDK bucket calls do not return a bucket CRN, so `status.crn` is intentionally left empty instead of copying the resource instance CRN and making status misleading.

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

The e2e suite deploys the operator into a real kind cluster and verifies that the manager and secured metrics endpoint are working. The metrics check uses `kubectl port-forward` and local `curl`, avoiding flaky public helper-image pulls.

## Roadmap

- Custom Prometheus metrics
- Validation webhook
- OLM/OpenShift bundle
- GitHub Actions polish
- Architecture diagram
- Screenshots of the MinIO demo
- Optional live IBM COS validation when verified IBM Cloud credentials are available

## Why This Project Matters

This project demonstrates practical platform engineering work:

- Designing a Kubernetes API
- Building an idempotent reconciliation loop
- Managing external resources safely with finalizers
- Handling credentials with Kubernetes Secrets
- Applying least-privilege RBAC
- Abstracting cloud providers behind clean Go interfaces
- Proving behavior with a zero-cost local object store
- Running CI that deploys the operator end to end

The full operator lifecycle is visible locally through MinIO, which makes the project easy to evaluate and explain without requiring access to paid cloud infrastructure.

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
