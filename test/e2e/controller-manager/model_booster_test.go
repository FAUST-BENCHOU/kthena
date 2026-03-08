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

package controller_manager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientset "github.com/volcano-sh/kthena/client-go/clientset/versioned"
	workload "github.com/volcano-sh/kthena/pkg/apis/workload/v1alpha1"
	"github.com/volcano-sh/kthena/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestModelCR creates a ModelBooster CR, waits for it to become active, and tests chat functionality.
func TestModelCR(t *testing.T) {
	testModelCRWithBackend(t, createTestModel())
}

// TestModelCRSglang creates a ModelBooster CR with SGLang backend, waits for it to become active, and tests chat.
func TestModelCRSglang(t *testing.T) {
	testModelCRWithBackend(t, createTestModelSglang())
}

func testModelCRWithBackend(t *testing.T, model *workload.ModelBooster) {
	ctx, kthenaClient := setupControllerManagerE2ETest(t)

	kubeConfig, err := utils.GetKubeConfig()
	require.NoError(t, err, "Failed to get kubeconfig")
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	require.NoError(t, err, "Failed to create kube client")

	createdModel, err := kthenaClient.WorkloadV1alpha1().ModelBoosters(testNamespace).Create(ctx, model, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create Model CR")
	assert.NotNil(t, createdModel)
	t.Logf("Created Model CR: %s/%s", createdModel.Namespace, createdModel.Name)

	// Cleanup model after test to free CPU for subsequent tests (e.g. TestModelCRSglang runs after TestModelCR)
	t.Cleanup(func() {
		_ = kthenaClient.WorkloadV1alpha1().ModelBoosters(testNamespace).Delete(ctx, model.Name, metav1.DeleteOptions{})
		// Wait for pods to be gone so resources are freed for next test
		selector := fmt.Sprintf("%s=%s", workload.ModelServingNameLabelKey, modelServingName)
		require.Eventually(t, func() bool {
			pods, err := kubeClient.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				return false
			}
			return len(pods.Items) == 0
		}, 90*time.Second, 3*time.Second, "Pods not terminated in time")
	})

	modelServingName := model.Name + "-" + model.Spec.Backend.Name

	require.Eventually(t, func() bool {
		m, err := kthenaClient.WorkloadV1alpha1().ModelBoosters(testNamespace).Get(ctx, model.Name, metav1.GetOptions{})
		if err != nil {
			t.Logf("Get model error: %v", err)
			return false
		}
		// Log Model status for debugging
		logModelStatus(t, m)
		// Log ModelServing status if exists
		logModelServingStatus(t, ctx, kthenaClient, testNamespace, modelServingName)
		// Log Pod status for ModelServing
		logModelServingPods(t, ctx, kubeClient, testNamespace, modelServingName)
		return meta.IsStatusConditionPresentAndEqual(m.Status.Conditions,
			string(workload.ModelStatusConditionTypeActive), metav1.ConditionTrue)
	}, 5*time.Minute, 5*time.Second, "Model did not become Active")

	messages := []utils.ChatMessage{utils.NewChatMessage("user", "Where is the capital of China?")}
	utils.CheckChatCompletions(t, model.Spec.Name, messages)
}

func logModelStatus(t *testing.T, m *workload.ModelBooster) {
	condStr := "none"
	if len(m.Status.Conditions) > 0 {
		var parts []string
		for _, c := range m.Status.Conditions {
			parts = append(parts, fmt.Sprintf("%s=%s(%s)", c.Type, c.Status, c.Reason))
		}
		condStr = fmt.Sprintf("%v", parts)
	}
	t.Logf("[Model %s] conditions: %s", m.Name, condStr)
}

func logModelServingStatus(t *testing.T, ctx context.Context, kthenaClient *clientset.Clientset, ns, name string) {
	ms, err := kthenaClient.WorkloadV1alpha1().ModelServings(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Logf("[ModelServing %s] not found: %v", name, err)
		return
	}
	condStr := "none"
	if len(ms.Status.Conditions) > 0 {
		var parts []string
		for _, c := range ms.Status.Conditions {
			parts = append(parts, fmt.Sprintf("%s=%s(%s)", c.Type, c.Status, c.Reason))
		}
		condStr = fmt.Sprintf("%v", parts)
	}
	t.Logf("[ModelServing %s] replicas=%d/%d, conditions: %s",
		name, ms.Status.AvailableReplicas, ms.Status.Replicas, condStr)
}

func logModelServingPods(t *testing.T, ctx context.Context, kubeClient kubernetes.Interface, ns, modelServingName string) {
	selector := fmt.Sprintf("%s=%s", workload.ModelServingNameLabelKey, modelServingName)
	pods, err := kubeClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Logf("[Pods for %s] list error: %v", modelServingName, err)
		return
	}
	for _, p := range pods.Items {
		phase := p.Status.Phase
		ready := "false"
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady {
				ready = string(c.Status)
				break
			}
		}
		var initStatuses []string
		for _, ics := range p.Status.InitContainerStatuses {
			state := "unknown"
			if ics.State.Waiting != nil {
				state = "Waiting:" + ics.State.Waiting.Reason
				if ics.State.Waiting.Message != "" {
					state += "(" + ics.State.Waiting.Message + ")"
				}
			} else if ics.State.Running != nil {
				state = "Running"
			} else if ics.State.Terminated != nil {
				state = "Terminated:" + ics.State.Terminated.Reason
			}
			initStatuses = append(initStatuses, fmt.Sprintf("%s=%s", ics.Name, state))
		}
		var containerStatuses []string
		for _, cs := range p.Status.ContainerStatuses {
			state := "unknown"
			if cs.State.Waiting != nil {
				state = "Waiting:" + cs.State.Waiting.Reason
				if cs.State.Waiting.Message != "" {
					state += "(" + cs.State.Waiting.Message + ")"
				}
			} else if cs.State.Running != nil {
				state = "Running"
			} else if cs.State.Terminated != nil {
				state = "Terminated:" + cs.State.Terminated.Reason
			}
			containerStatuses = append(containerStatuses, fmt.Sprintf("%s=%s", cs.Name, state))
		}
		t.Logf("[Pod %s] phase=%s ready=%s initContainers: %v containers: %v", p.Name, phase, ready, initStatuses, containerStatuses)
		// Log pod events when Pending to help debug scheduling/startup issues
		if phase == corev1.PodPending {
			events, err := kubeClient.CoreV1().Events(ns).List(ctx, metav1.ListOptions{FieldSelector: fmt.Sprintf("involvedObject.name=%s", p.Name)})
			if err == nil && len(events.Items) > 0 {
				for _, e := range events.Items {
					t.Logf("[Pod %s] event: %s %s: %s", p.Name, e.Reason, e.Type, e.Message)
				}
			}
		}
	}
	if len(pods.Items) == 0 {
		t.Logf("[Pods for %s] no pods found (selector: %s)", modelServingName, selector)
	}
}

func createValidModelBoosterForWebhookTest() *workload.ModelBooster {
	model := createTestModel()
	model.Name = "webhook-test-model"
	model.Spec.Name = "webhook-test-model"
	return model
}

func createTestModel() *workload.ModelBooster {
	// Create a simple config as JSON
	config := &apiextensionsv1.JSON{}
	configRaw := `{
		"served-model-name": "test-model",
		"max-model-len": 32768,
		"max-num-batched-tokens": 65536,
		"block-size": 128,
		"enable-prefix-caching": ""
	}`
	config.Raw = []byte(configRaw)

	return &workload.ModelBooster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: testNamespace,
		},
		Spec: workload.ModelBoosterSpec{
			Name: "test-model",
			Backend: workload.ModelBackend{
				Name:        "backend1",
				Type:        workload.ModelBackendTypeVLLM,
				ModelURI:    "hf://Qwen/Qwen2.5-0.5B-Instruct",
				CacheURI:    "hostpath:///tmp/cache",
				MinReplicas: 1,
				MaxReplicas: 1,
				Workers: []workload.ModelWorker{
					{
						Type:      workload.ModelWorkerTypeServer,
						Image:     "ghcr.io/huntersman/vllm-cpu-env:latest",
						Replicas:  1,
						Pods:      1,
						Config:    *config,
						Resources: corev1ResourceRequirements(),
					},
				},
			},
		},
	}
}

func createTestModelSglang() *workload.ModelBooster {
	config := &apiextensionsv1.JSON{}
	configRaw := `{
		"device": "cpu",
		"mem-fraction-static": "0.9"
	}`
	config.Raw = []byte(configRaw)

	return &workload.ModelBooster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model-sglang",
			Namespace: testNamespace,
		},
		Spec: workload.ModelBoosterSpec{
			Name: "test-model-sglang",
			Backend: workload.ModelBackend{
				Name:        "backend1",
				Type:        workload.ModelBackendTypeSGLang,
				ModelURI:    "hf://Qwen/Qwen2.5-0.5B-Instruct",
				CacheURI:    "hostpath:///tmp/cache",
				MinReplicas: 1,
				MaxReplicas: 1,
				Workers: []workload.ModelWorker{
					{
						Type:      workload.ModelWorkerTypeServer,
						Image:     "metaphorprojects/sglang-cpu:latest",
						Replicas:  1,
						Pods:      1,
						Config:    *config,
						Resources: sglangResourceRequirements(),
					},
				},
			},
		},
	}
}

// sglangResourceRequirements returns lighter resources for SGLang e2e to fit CI (runs after TestModelCR which already consumes CPU).
func sglangResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		},
	}
}

func createInvalidModel() *workload.ModelBooster {
	// Create a simple config as JSON
	config := &apiextensionsv1.JSON{}
	configRaw := `{
		"served-model-name": "invalid-model",
		"max-model-len": 32768,
		"max-num-batched-tokens": 65536,
		"block-size": 128,
		"enable-prefix-caching": ""
	}`
	config.Raw = []byte(configRaw)

	return &workload.ModelBooster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-model",
			Namespace: testNamespace,
		},
		Spec: workload.ModelBoosterSpec{
			Name: "invalid-model",
			Backend: workload.ModelBackend{
				Name:        "backend1",
				Type:        workload.ModelBackendTypeVLLM,
				ModelURI:    "hf://Qwen/Qwen2.5-0.5B-Instruct",
				CacheURI:    "hostpath:///tmp/cache",
				MinReplicas: 5, // invalid: greater than maxReplicas
				MaxReplicas: 1,
				Workers: []workload.ModelWorker{
					{
						Type:      workload.ModelWorkerTypeServer,
						Image:     "ghcr.io/huntersman/vllm-cpu-env:latest",
						Replicas:  1,
						Pods:      1,
						Config:    *config,
						Resources: corev1ResourceRequirements(),
					},
				},
			},
		},
	}
}

// corev1ResourceRequirements is a helper to avoid duplication and keep imports clean
func corev1ResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
		},
	}
}
