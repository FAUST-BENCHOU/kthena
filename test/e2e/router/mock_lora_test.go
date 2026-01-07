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

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestMockServiceLoRASupport tests whether the mock service (ghcr.io/yaozengzeng/vllm-mock:latest)
// supports LoRA adapter model names like "lora-A" and "lora-B".
// This test directly sends requests to the Pod to verify if the mock service can handle LoRA model names.
func TestMockServiceLoRASupport(t *testing.T) {
	ctx := context.Background()

	// Get Pods for deepseek-r1-1-5b deployment
	t.Log("Getting Pods for deepseek-r1-1-5b deployment...")
	pods, err := testCtx.KubeClient.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=deepseek-r1-1-5b",
	})
	require.NoError(t, err, "Failed to list Pods")
	require.NotEmpty(t, pods.Items, "No Pods found for deepseek-r1-1-5b")

	// Find a running Pod
	var targetPod *corev1.Pod
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			targetPod = &pods.Items[i]
			break
		}
	}
	require.NotNil(t, targetPod, "No running Pod found for deepseek-r1-1-5b")
	require.NotEmpty(t, targetPod.Status.PodIP, "Pod does not have an IP address")

	t.Logf("Testing mock service on Pod: %s/%s (IP: %s)", targetPod.Namespace, targetPod.Name, targetPod.Status.PodIP)

	// Test 1: Verify Pod responds to base model name
	t.Run("BaseModelName", func(t *testing.T) {
		baseModelName := "deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B"
		resp := sendDirectRequestToPod(t, targetPod.Status.PodIP, 8000, baseModelName)
		assert.Equal(t, http.StatusOK, resp.StatusCode, "Base model name should return 200")
		t.Logf("Base model name test passed: status=%d, body=%s", resp.StatusCode, resp.Body)
	})

	// Test 2: Test with "lora-A" model name
	t.Run("LoRAAdapterA", func(t *testing.T) {
		loraModelName := "lora-A"
		resp := sendDirectRequestToPod(t, targetPod.Status.PodIP, 8000, loraModelName)
		t.Logf("LoRA-A test result: status=%d, body=%s", resp.StatusCode, resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Logf("WARNING: Mock service does not support LoRA adapter model name 'lora-A'")
			t.Logf("Response status: %d, Response body: %s", resp.StatusCode, resp.Body)
		} else {
			t.Logf("SUCCESS: Mock service supports LoRA adapter model name 'lora-A'")
		}
	})

	// Test 3: Test with "lora-B" model name
	t.Run("LoRAAdapterB", func(t *testing.T) {
		loraModelName := "lora-B"
		resp := sendDirectRequestToPod(t, targetPod.Status.PodIP, 8000, loraModelName)
		t.Logf("LoRA-B test result: status=%d, body=%s", resp.StatusCode, resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Logf("WARNING: Mock service does not support LoRA adapter model name 'lora-B'")
			t.Logf("Response status: %d, Response body: %s", resp.StatusCode, resp.Body)
		} else {
			t.Logf("SUCCESS: Mock service supports LoRA adapter model name 'lora-B'")
		}
	})

	// Test 4: Test with a random model name to see how mock service handles unknown models
	t.Run("UnknownModelName", func(t *testing.T) {
		unknownModelName := "unknown-model-12345"
		resp := sendDirectRequestToPod(t, targetPod.Status.PodIP, 8000, unknownModelName)
		t.Logf("Unknown model test result: status=%d, body=%s", resp.StatusCode, resp.Body)
	})
}

// sendDirectRequestToPod sends a chat completions request directly to a Pod
func sendDirectRequestToPod(t *testing.T, podIP string, port int32, modelName string) *DirectPodResponse {
	url := fmt.Sprintf("http://%s:%d/v1/chat/completions", podIP, port)

	requestBody := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "Hello",
			},
		},
		"stream": false,
	}

	jsonData, err := json.Marshal(requestBody)
	require.NoError(t, err, "Failed to marshal request body")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return &DirectPodResponse{
			StatusCode: 0,
			Body:       fmt.Sprintf("Request failed: %v", err),
			Error:      err,
		}
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &DirectPodResponse{
			StatusCode: resp.StatusCode,
			Body:       fmt.Sprintf("Failed to read response: %v", err),
			Error:      err,
		}
	}

	return &DirectPodResponse{
		StatusCode: resp.StatusCode,
		Body:       string(responseBody),
		Error:      nil,
	}
}

// DirectPodResponse represents the response from a direct Pod request
type DirectPodResponse struct {
	StatusCode int
	Body       string
	Error      error
}
