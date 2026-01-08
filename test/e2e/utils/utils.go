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
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientset "github.com/volcano-sh/kthena/client-go/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
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

// GetPodLogs retrieves logs from a pod. It returns logs since the specified time, or all logs if sinceTime is nil.
func GetPodLogs(ctx context.Context, kubeClient kubernetes.Interface, namespace, podName string, containerName string, sinceTime *metav1.Time) (string, error) {
	req := kubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		SinceTime: sinceTime,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	buf := new(strings.Builder)
	_, err = io.Copy(buf, stream)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// AccessLogFields represents the parsed fields from an access log entry
type AccessLogFields struct {
	ModelRoute    string
	ModelServer   string
	Gateway       string
	HTTPRoute     string
	InferencePool string
	ModelName     string
	SelectedPod   string
}

// ParseAccessLogLine parses a text-format access log line and extracts relevant fields.
// The format is: [timestamp] "METHOD /path PROTOCOL" status_code [error=...] model_name=... model_route=... etc.
func ParseAccessLogLine(line string) *AccessLogFields {
	fields := &AccessLogFields{}

	// Extract model_route field
	if match := regexp.MustCompile(`model_route=([^\s]+)`).FindStringSubmatch(line); len(match) > 1 {
		fields.ModelRoute = match[1]
	}

	// Extract model_server field
	if match := regexp.MustCompile(`model_server=([^\s]+)`).FindStringSubmatch(line); len(match) > 1 {
		fields.ModelServer = match[1]
	}

	// Extract gateway field
	if match := regexp.MustCompile(`gateway=([^\s]+)`).FindStringSubmatch(line); len(match) > 1 {
		fields.Gateway = match[1]
	}

	// Extract http_route field
	if match := regexp.MustCompile(`http_route=([^\s]+)`).FindStringSubmatch(line); len(match) > 1 {
		fields.HTTPRoute = match[1]
	}

	// Extract inference_pool field
	if match := regexp.MustCompile(`inference_pool=([^\s]+)`).FindStringSubmatch(line); len(match) > 1 {
		fields.InferencePool = match[1]
	}

	// Extract model_name field
	if match := regexp.MustCompile(`model_name=([^\s]+)`).FindStringSubmatch(line); len(match) > 1 {
		fields.ModelName = match[1]
	}

	// Extract selected_pod field
	if match := regexp.MustCompile(`selected_pod=([^\s]+)`).FindStringSubmatch(line); len(match) > 1 {
		fields.SelectedPod = match[1]
	}

	return fields
}

// FindAccessLogEntries finds all access log entries in the logs that match the specified criteria.
// It looks for entries containing the specified model name and path.
func FindAccessLogEntries(logs string, modelName string, path string) []*AccessLogFields {
	var entries []*AccessLogFields
	lines := strings.Split(logs, "\n")

	for _, line := range lines {
		// Check if this line contains the model name and path
		if strings.Contains(line, modelName) && strings.Contains(line, path) {
			parsed := ParseAccessLogLine(line)
			entries = append(entries, parsed)
		}
	}

	return entries
}
