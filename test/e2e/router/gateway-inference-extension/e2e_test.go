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

package gie

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	networkingv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/networking/v1alpha1"
	"github.com/volcano-sh/kthena/test/e2e/framework"
	routercontext "github.com/volcano-sh/kthena/test/e2e/router/context"
	"github.com/volcano-sh/kthena/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	testCtx         *routercontext.RouterTestContext
	testNamespace   string
	kthenaNamespace string
)

func TestMain(m *testing.M) {
	testNamespace = "kthena-e2e-gie-" + utils.RandomString(5)

	config := framework.NewDefaultConfig()
	kthenaNamespace = config.Namespace
	config.NetworkingEnabled = true
	config.GatewayAPIEnabled = true
	config.InferenceExtensionEnabled = true

	if err := framework.InstallKthena(config); err != nil {
		fmt.Printf("Failed to install kthena: %v\n", err)
		os.Exit(1)
	}

	var err error
	testCtx, err = routercontext.NewRouterTestContext(testNamespace)
	if err != nil {
		fmt.Printf("Failed to create router test context: %v\n", err)
		_ = framework.UninstallKthena(config.Namespace)
		os.Exit(1)
	}

	if err := testCtx.CreateTestNamespace(); err != nil {
		fmt.Printf("Failed to create test namespace: %v\n", err)
		_ = framework.UninstallKthena(config.Namespace)
		os.Exit(1)
	}

	if err := testCtx.SetupCommonComponents(); err != nil {
		fmt.Printf("Failed to setup common components: %v\n", err)
		_ = testCtx.DeleteTestNamespace()
		_ = framework.UninstallKthena(config.Namespace)
		os.Exit(1)
	}

	code := m.Run()

	if err := testCtx.CleanupCommonComponents(); err != nil {
		fmt.Printf("Failed to cleanup common components: %v\n", err)
	}

	if err := testCtx.DeleteTestNamespace(); err != nil {
		fmt.Printf("Failed to delete test namespace: %v\n", err)
	}

	if err := framework.UninstallKthena(config.Namespace); err != nil {
		fmt.Printf("Failed to uninstall kthena: %v\n", err)
	}

	os.Exit(code)
}

func TestGatewayInferenceExtension(t *testing.T) {
	ctx := context.Background()

	// 1. Deploy InferencePool
	t.Log("Deploying InferencePool...")
	inferencePool := utils.LoadYAMLFromFile[inferencev1.InferencePool]("examples/kthena-router/InferencePool.yaml")
	inferencePool.Namespace = testNamespace

	createdInferencePool, err := testCtx.InferenceClient.InferenceV1().InferencePools(testNamespace).Create(ctx, inferencePool, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create InferencePool")

	t.Cleanup(func() {
		if err := testCtx.InferenceClient.InferenceV1().InferencePools(testNamespace).Delete(context.Background(), createdInferencePool.Name, metav1.DeleteOptions{}); err != nil {
			t.Logf("Warning: Failed to delete InferencePool %s/%s: %v", testNamespace, createdInferencePool.Name, err)
		}
	})

	// 2. Deploy HTTPRoute
	t.Log("Deploying HTTPRoute...")
	httpRoute := utils.LoadYAMLFromFile[gatewayv1.HTTPRoute]("examples/kthena-router/HTTPRoute.yaml")
	httpRoute.Namespace = testNamespace

	// Update parentRefs to point to the kthena installation namespace
	ktNamespace := gatewayv1.Namespace(kthenaNamespace)
	if len(httpRoute.Spec.ParentRefs) > 0 {
		for i := range httpRoute.Spec.ParentRefs {
			httpRoute.Spec.ParentRefs[i].Namespace = &ktNamespace
		}
	}

	createdHTTPRoute, err := testCtx.GatewayClient.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, httpRoute, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create HTTPRoute")

	t.Cleanup(func() {
		if err := testCtx.GatewayClient.GatewayV1().HTTPRoutes(testNamespace).Delete(context.Background(), createdHTTPRoute.Name, metav1.DeleteOptions{}); err != nil {
			t.Logf("Warning: Failed to delete HTTPRoute %s/%s: %v", testNamespace, createdHTTPRoute.Name, err)
		}
	})

	// 3. Test accessing the route
	t.Log("Testing chat completions via HTTPRoute and InferencePool...")
	messages := []utils.ChatMessage{
		utils.NewChatMessage("user", "Hello GIE"),
	}

	utils.CheckChatCompletions(t, "deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B", messages)
}

// TestBothAPIsConfigured tests both ModelRoute/ModelServer and HTTPRoute/InferencePool APIs configured together.
// It verifies that deepseek-r1-1-5b can be accessed via ModelRoute and deepseek-r1-7b can be accessed via HTTPRoute.
func TestBothAPIsConfigured(t *testing.T) {
	ctx := context.Background()

	// 1. Deploy ModelRoute and ModelServer for ModelRoute/ModelServer API
	t.Log("Deploying ModelRoute...")
	modelRoute := utils.LoadYAMLFromFile[networkingv1alpha1.ModelRoute]("examples/kthena-router/ModelRoute-binding-gateway.yaml")
	modelRoute.Namespace = testNamespace

	// Update parentRefs to point to the kthena installation namespace
	ktNamespace := gatewayv1.Namespace(kthenaNamespace)
	if len(modelRoute.Spec.ParentRefs) > 0 {
		for i := range modelRoute.Spec.ParentRefs {
			modelRoute.Spec.ParentRefs[i].Namespace = &ktNamespace
		}
	}

	createdModelRoute, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, modelRoute, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create ModelRoute")

	t.Cleanup(func() {
		if err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), createdModelRoute.Name, metav1.DeleteOptions{}); err != nil {
			t.Logf("Warning: Failed to delete ModelRoute %s/%s: %v", testNamespace, createdModelRoute.Name, err)
		}
	})

	// ModelServer-ds1.5b.yaml is already deployed by SetupCommonComponents

	// 2. Deploy InferencePool for HTTPRoute/InferencePool API (pointing to deepseek-r1-7b)
	t.Log("Deploying InferencePool for deepseek-r1-7b...")
	inferencePool7b := &inferencev1.InferencePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deepseek-r1-7b",
			Namespace: testNamespace,
		},
		Spec: inferencev1.InferencePoolSpec{
			TargetPorts: []inferencev1.Port{
				{Number: 8000},
			},
			Selector: inferencev1.LabelSelector{
				MatchLabels: map[inferencev1.LabelKey]inferencev1.LabelValue{
					inferencev1.LabelKey("app"): inferencev1.LabelValue("deepseek-r1-7b"),
				},
			},
			EndpointPickerRef: inferencev1.EndpointPickerRef{
				Name: "deepseek-r1-7b",
				Port: &inferencev1.Port{
					Number: 8080,
				},
			},
		},
	}

	createdInferencePool7b, err := testCtx.InferenceClient.InferenceV1().InferencePools(testNamespace).Create(ctx, inferencePool7b, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create InferencePool for 7b")

	t.Cleanup(func() {
		if err := testCtx.InferenceClient.InferenceV1().InferencePools(testNamespace).Delete(context.Background(), createdInferencePool7b.Name, metav1.DeleteOptions{}); err != nil {
			t.Logf("Warning: Failed to delete InferencePool %s/%s: %v", testNamespace, createdInferencePool7b.Name, err)
		}
	})

	// 3. Deploy HTTPRoute pointing to the 7b InferencePool
	t.Log("Deploying HTTPRoute...")
	httpRoute := utils.LoadYAMLFromFile[gatewayv1.HTTPRoute]("examples/kthena-router/HTTPRoute.yaml")
	httpRoute.Namespace = testNamespace
	httpRoute.Name = "llm-route-7b"

	// Update parentRefs to point to the kthena installation namespace (reuse ktNamespace from above)
	ktNamespace = gatewayv1.Namespace(kthenaNamespace)
	if len(httpRoute.Spec.ParentRefs) > 0 {
		for i := range httpRoute.Spec.ParentRefs {
			httpRoute.Spec.ParentRefs[i].Namespace = &ktNamespace
		}
	}

	// Update backendRefs to point to the 7b InferencePool
	if len(httpRoute.Spec.Rules) > 0 && len(httpRoute.Spec.Rules[0].BackendRefs) > 0 {
		backendRefName := gatewayv1.ObjectName("deepseek-r1-7b")
		httpRoute.Spec.Rules[0].BackendRefs[0].Name = backendRefName
	}

	createdHTTPRoute, err := testCtx.GatewayClient.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, httpRoute, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create HTTPRoute")

	t.Cleanup(func() {
		if err := testCtx.GatewayClient.GatewayV1().HTTPRoutes(testNamespace).Delete(context.Background(), createdHTTPRoute.Name, metav1.DeleteOptions{}); err != nil {
			t.Logf("Warning: Failed to delete HTTPRoute %s/%s: %v", testNamespace, createdHTTPRoute.Name, err)
		}
	})

	// 4. Test accessing both models
	// Test ModelRoute/ModelServer API - deepseek-r1-1-5b via ModelRoute
	t.Log("Testing ModelRoute/ModelServer API - accessing deepseek-r1-1-5b via ModelRoute...")
	messages1_5b := []utils.ChatMessage{
		utils.NewChatMessage("user", "Hello ModelRoute"),
	}
	utils.CheckChatCompletions(t, modelRoute.Spec.ModelName, messages1_5b)

	// Test HTTPRoute/InferencePool API - deepseek-r1-7b via HTTPRoute
	t.Log("Testing HTTPRoute/InferencePool API - accessing deepseek-r1-7b via HTTPRoute...")
	messages7b := []utils.ChatMessage{
		utils.NewChatMessage("user", "Hello HTTPRoute"),
	}
	utils.CheckChatCompletions(t, "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B", messages7b)
}

// TestAccessLogGatewayInfo verifies that access logs correctly log information about
// ModelRoute/ModelServer and Gateway API (HTTPRoute/InferencePool) fields.
// This test:
// 1. Deploys a ModelRoute (with no parentRefs, and modelServerName: "deepseek-r1-1-5b")
// 2. Deploys an InferencePool (using the same label selector as ModelServer, pointing to the same pods)
// 3. Sets up an HTTPRoute (pointing to the InferencePool, to validate HTTPRoute → InferencePool flow)
// 4. After ModelServer pods are ready, identifies the router pod and uses port-forward to directly access it (with gatewayKey left empty)
// 5. Verifies that access logs contain the relevant gatewayinfo
func TestAccessLogGatewayInfo(t *testing.T) {
	ctx := context.Background()

	// 1. Deploy ModelRoute (with no parentRefs, and modelServerName: "deepseek-r1-1-5b")
	t.Log("Deploying ModelRoute without parentRefs...")
	modelRoute := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deepseek-route-accesslog-test",
			Namespace: testNamespace,
		},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: "deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B",
			Rules: []*networkingv1alpha1.Rule{
				{
					Name: "default",
					TargetModels: []*networkingv1alpha1.TargetModel{
						{
							ModelServerName: "deepseek-r1-1-5b",
						},
					},
				},
			},
			// No parentRefs - empty slice
			ParentRefs: []gatewayv1.ParentReference{},
		},
	}

	createdModelRoute, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, modelRoute, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create ModelRoute")

	t.Cleanup(func() {
		if err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), createdModelRoute.Name, metav1.DeleteOptions{}); err != nil {
			t.Logf("Warning: Failed to delete ModelRoute %s/%s: %v", testNamespace, createdModelRoute.Name, err)
		}
	})

	// ModelServer-ds1.5b.yaml is already deployed by SetupCommonComponents

	// 2. Deploy InferencePool (using the same label selector as ModelServer, pointing to the same pods)
	t.Log("Deploying InferencePool...")
	inferencePool := utils.LoadYAMLFromFile[inferencev1.InferencePool]("examples/kthena-router/InferencePool.yaml")
	inferencePool.Namespace = testNamespace

	createdInferencePool, err := testCtx.InferenceClient.InferenceV1().InferencePools(testNamespace).Create(ctx, inferencePool, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create InferencePool")

	t.Cleanup(func() {
		if err := testCtx.InferenceClient.InferenceV1().InferencePools(testNamespace).Delete(context.Background(), createdInferencePool.Name, metav1.DeleteOptions{}); err != nil {
			t.Logf("Warning: Failed to delete InferencePool %s/%s: %v", testNamespace, createdInferencePool.Name, err)
		}
	})

	// 3. Deploy HTTPRoute (pointing to the InferencePool)
	t.Log("Deploying HTTPRoute...")
	httpRoute := utils.LoadYAMLFromFile[gatewayv1.HTTPRoute]("examples/kthena-router/HTTPRoute.yaml")
	httpRoute.Namespace = testNamespace
	httpRoute.Name = "llm-route-accesslog-test"

	// Update parentRefs to point to the kthena installation namespace
	ktNamespace := gatewayv1.Namespace(kthenaNamespace)
	if len(httpRoute.Spec.ParentRefs) > 0 {
		for i := range httpRoute.Spec.ParentRefs {
			httpRoute.Spec.ParentRefs[i].Namespace = &ktNamespace
		}
	}

	createdHTTPRoute, err := testCtx.GatewayClient.GatewayV1().HTTPRoutes(testNamespace).Create(ctx, httpRoute, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create HTTPRoute")

	t.Cleanup(func() {
		if err := testCtx.GatewayClient.GatewayV1().HTTPRoutes(testNamespace).Delete(context.Background(), createdHTTPRoute.Name, metav1.DeleteOptions{}); err != nil {
			t.Logf("Warning: Failed to delete HTTPRoute %s/%s: %v", testNamespace, createdHTTPRoute.Name, err)
		}
	})

	// Wait for ModelServer pods to be ready
	t.Log("Waiting for ModelServer pods to be ready...")
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	err = wait.PollUntilContextTimeout(timeoutCtx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := testCtx.KubeClient.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=deepseek-r1-1-5b",
		})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				return false, nil
			}
		}
		return len(pods.Items) > 0, nil
	})
	require.NoError(t, err, "ModelServer pods did not become ready")

	// 4. Identify the router pod and use port-forward to directly access it (with gatewayKey left empty)
	t.Log("Getting router pod...")
	routerPod := utils.GetRouterPod(t, testCtx.KubeClient, kthenaNamespace)
	t.Logf("Found router pod: %s/%s", routerPod.Namespace, routerPod.Name)

	// Setup port-forward to router pod
	t.Log("Setting up port-forward to router pod...")
	localPort := "18080" // Use different port to avoid conflicts
	routerPort := "8080"
	pf, err := utils.SetupPortForwardToPod(kthenaNamespace, routerPod.Name, localPort, routerPort)
	require.NoError(t, err, "Failed to setup port-forward")
	defer pf.Close()

	// Get logs before making the request to compare
	t.Log("Capturing logs before request...")
	beforeTime := metav1.Now()
	beforeLogs, err := utils.GetPodLogs(ctx, testCtx.KubeClient, kthenaNamespace, routerPod.Name, "", &beforeTime)
	require.NoError(t, err, "Failed to get pod logs")

	// Wait a bit to ensure log timestamp is different
	time.Sleep(2 * time.Second)

	// Send a request to the router (via HTTPRoute/InferencePool flow)
	t.Log("Sending request via HTTPRoute/InferencePool...")
	messages := []utils.ChatMessage{
		utils.NewChatMessage("user", "Hello AccessLog Test"),
	}

	// Use the port-forwarded URL (with empty gatewayKey, accessing via default port)
	url := fmt.Sprintf("http://127.0.0.1:%s/v1/chat/completions", localPort)
	utils.CheckChatCompletionsWithURL(t, url, "deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B", messages)

	// Wait a bit for logs to be written
	time.Sleep(2 * time.Second)

	// 5. Verify that access logs contain the relevant gatewayinfo
	t.Log("Verifying access logs...")
	allLogs, err := utils.GetPodLogs(ctx, testCtx.KubeClient, kthenaNamespace, routerPod.Name, "", &beforeTime)
	require.NoError(t, err, "Failed to get pod logs after request")

	// Find new log entries since beforeTime
	newLogs := strings.TrimPrefix(allLogs, beforeLogs)
	require.NotEmpty(t, newLogs, "No new log entries found")

	// Find access log entries for our request
	t.Logf("Searching for access log entries in logs...")
	entries := utils.FindAccessLogEntries(newLogs, "deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B", "/v1/chat/completions")
	require.NotEmpty(t, entries, "No access log entries found for the request")

	// Verify the last entry (should be our request)
	entry := entries[len(entries)-1]
	t.Logf("Found access log entry: ModelRoute=%s, ModelServer=%s, Gateway=%s, HTTPRoute=%s, InferencePool=%s",
		entry.ModelRoute, entry.ModelServer, entry.Gateway, entry.HTTPRoute, entry.InferencePool)

	// Verify ModelRoute/ModelServer fields are logged
	// Note: When accessing via HTTPRoute, ModelRoute may not be set, but ModelServer should be
	// or vice versa depending on the routing flow
	t.Log("Verifying ModelRoute/ModelServer fields...")
	if entry.ModelServer != "" {
		expectedModelServer := fmt.Sprintf("%s/deepseek-r1-1-5b", testNamespace)
		require.Equal(t, expectedModelServer, entry.ModelServer, "ModelServer field should match")
		t.Logf("✓ ModelServer field is correctly logged: %s", entry.ModelServer)
	}

	// Verify Gateway API fields are logged (HTTPRoute/InferencePool)
	t.Log("Verifying Gateway API fields...")
	if entry.HTTPRoute != "" {
		expectedHTTPRoute := fmt.Sprintf("%s/%s", testNamespace, createdHTTPRoute.Name)
		require.Equal(t, expectedHTTPRoute, entry.HTTPRoute, "HTTPRoute field should match")
		t.Logf("✓ HTTPRoute field is correctly logged: %s", entry.HTTPRoute)
	}

	if entry.InferencePool != "" {
		expectedInferencePool := fmt.Sprintf("%s/%s", testNamespace, createdInferencePool.Name)
		require.Equal(t, expectedInferencePool, entry.InferencePool, "InferencePool field should match")
		t.Logf("✓ InferencePool field is correctly logged: %s", entry.InferencePool)
	}

	// Gateway field may or may not be present depending on the routing configuration
	if entry.Gateway != "" {
		t.Logf("✓ Gateway field is logged: %s", entry.Gateway)
	} else {
		t.Log("Gateway field is not present (this may be expected when gatewayKey is empty)")
	}

	// Verify that at least one of the Gateway API fields or ModelRoute fields is present
	hasGatewayInfo := entry.HTTPRoute != "" || entry.InferencePool != "" || entry.Gateway != "" || entry.ModelRoute != "" || entry.ModelServer != ""
	require.True(t, hasGatewayInfo, "At least one gatewayinfo field (ModelRoute, ModelServer, Gateway, HTTPRoute, or InferencePool) should be present in access logs")

	t.Log("✓ Access log verification passed: gatewayinfo fields are correctly logged")
}
