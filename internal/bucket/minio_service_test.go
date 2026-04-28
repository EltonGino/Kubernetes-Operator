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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMinIOCredentialsFromSecret(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			minIOEndpointKey:  []byte("localhost:9000"),
			minIOAccessKeyKey: []byte("minioadmin"),
			minIOSecretKeyKey: []byte("minioadmin"),
			minIOUseSSLKey:    []byte("false"),
		},
	}

	credentials, err := MinIOCredentialsFromSecret(secret)
	if err != nil {
		t.Fatalf("MinIOCredentialsFromSecret returned error: %v", err)
	}

	if credentials.Endpoint != "localhost:9000" {
		t.Fatalf("Endpoint = %q, want localhost:9000", credentials.Endpoint)
	}
	if credentials.AccessKey != "minioadmin" {
		t.Fatalf("AccessKey = %q, want minioadmin", credentials.AccessKey)
	}
	if credentials.SecretKey != "minioadmin" {
		t.Fatalf("SecretKey = %q, want minioadmin", credentials.SecretKey)
	}
	if credentials.UseSSL {
		t.Fatal("UseSSL = true, want false")
	}
}

func TestMinIOCredentialsFromSecretRequiresExpectedKeys(t *testing.T) {
	_, err := MinIOCredentialsFromSecret(&corev1.Secret{
		Data: map[string][]byte{
			minIOEndpointKey:  []byte("localhost:9000"),
			minIOAccessKeyKey: []byte("minioadmin"),
			minIOUseSSLKey:    []byte("false"),
		},
	})
	if err == nil {
		t.Fatal("MinIOCredentialsFromSecret returned nil error for missing secretKey")
	}
	if !strings.Contains(err.Error(), `missing required key "secretKey"`) {
		t.Fatalf("error = %q, want missing secretKey message", err.Error())
	}
}

func TestMinIOCredentialsFromSecretDoesNotExposeValues(t *testing.T) {
	_, err := MinIOCredentialsFromSecret(&corev1.Secret{
		Data: map[string][]byte{
			minIOEndpointKey:  []byte("http://localhost:9000"),
			minIOAccessKeyKey: []byte("very-sensitive-access-key"),
			minIOSecretKeyKey: []byte("very-sensitive-secret-key"),
			minIOUseSSLKey:    []byte("definitely-not-a-bool"),
		},
	})
	if err == nil {
		t.Fatal("MinIOCredentialsFromSecret returned nil error for invalid values")
	}

	errorText := err.Error()
	for _, sensitive := range []string{
		"very-sensitive-access-key",
		"very-sensitive-secret-key",
		"definitely-not-a-bool",
		"http://localhost:9000",
	} {
		if strings.Contains(errorText, sensitive) {
			t.Fatalf("error %q exposed sensitive value %q", errorText, sensitive)
		}
	}
}
