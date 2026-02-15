/*
Copyright The Volcano Authors.

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

package utils

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientset "github.com/volcano-sh/kthena/client-go/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// WaitForModelServingReady waits for a ModelServing to become ready by checking
// if all expected replicas are available.
func WaitForModelServingReady(t *testing.T, ctx context.Context, kthenaClient *clientset.Clientset, namespace, name string) {
	t.Log("Waiting for ModelServing to be ready...")
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	err := wait.PollUntilContextTimeout(timeoutCtx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		ms, err := kthenaClient.WorkloadV1alpha1().ModelServings(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Logf("Error getting ModelServing %s, retrying: %v", name, err)
			return false, err
		}
		// Check if all replicas are available
		expectedReplicas := int32(1)
		if ms.Spec.Replicas != nil {
			expectedReplicas = *ms.Spec.Replicas
		}
		return ms.Status.AvailableReplicas >= expectedReplicas, nil
	})
	require.NoError(t, err, "ModelServing did not become ready")
}

// WaitForModelServingReadyWithProgressExtend waits for ModelServing to become ready.
// The deadline extends by 2min each time AvailableReplicas increases, up to 15min hard max.
func WaitForModelServingReadyWithProgressExtend(t *testing.T, ctx context.Context, kthenaClient *clientset.Clientset, namespace, name string) {
	t.Log("Waiting for ModelServing to be ready (with progress-based deadline extension)...")
	start := time.Now()
	initialDeadline := start.Add(5 * time.Minute)
	hardDeadline := start.Add(15 * time.Minute)
	deadline := initialDeadline
	lastAvailable := int32(-1)

	timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	err := wait.PollUntilContextTimeout(timeoutCtx, 5*time.Second, 15*time.Minute, true, func(ctx context.Context) (bool, error) {
		now := time.Now()
		if now.After(deadline) {
			return false, fmt.Errorf("deadline exceeded: ModelServing did not become ready within timeout (last extended: %v)", deadline.Sub(start))
		}

		ms, err := kthenaClient.WorkloadV1alpha1().ModelServings(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Logf("Error getting ModelServing %s, retrying: %v", name, err)
			return false, err
		}

		expectedReplicas := int32(1)
		if ms.Spec.Replicas != nil {
			expectedReplicas = *ms.Spec.Replicas
		}

		if ms.Status.AvailableReplicas >= expectedReplicas {
			return true, nil
		}

		if ms.Status.AvailableReplicas > lastAvailable {
			lastAvailable = ms.Status.AvailableReplicas
			extended := now.Add(2 * time.Minute)
			newDeadline := extended
			if initialDeadline.After(extended) {
				newDeadline = initialDeadline
			}
			if newDeadline.After(deadline) {
				deadline = newDeadline
				if deadline.After(hardDeadline) {
					deadline = hardDeadline
				}
				t.Logf("Progress: %d/%d replicas ready, deadline extended to %v", lastAvailable, expectedReplicas, deadline.Sub(start))
			}
		}

		return false, nil
	})
	require.NoError(t, err, "ModelServing did not become ready")
}
