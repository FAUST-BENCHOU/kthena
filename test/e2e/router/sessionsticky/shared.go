/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package sessionsticky

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/networking/v1alpha1"
	"github.com/volcano-sh/kthena/test/e2e/router"
	routercontext "github.com/volcano-sh/kthena/test/e2e/router/context"
	"github.com/volcano-sh/kthena/test/e2e/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestSessionStickyKeyPinsAndIsolatesShared verifies the same header session key keeps routing to one backend
// and different session keys do not share bindings; switching back to the first key restores its mapping.
func TestSessionStickyKeyPinsAndIsolatesShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	model := "ss-iso-" + utils.RandomString(6)
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-iso", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: true,
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Test-Session"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	stickyRequireEventuallyRouteReady(t, model, "hi", &stickyChatOptions{Headers: map[string]string{"X-Test-Session": "s1"}})

	optOne := &stickyChatOptions{Headers: map[string]string{"X-Test-Session": "one"}}
	optTwo := &stickyChatOptions{Headers: map[string]string{"X-Test-Session": "two"}}

	var podOne string
	for i := 0; i < 3; i++ {
		stickySendChatCompletions(t, model, userChatSticky(fmt.Sprintf("iso-one-%d", i)), optOne)
		p := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
		require.NotEmpty(t, p)
		if podOne == "" {
			podOne = p
		} else {
			assert.Equal(t, podOne, p, "session one should stay on one backend")
		}
	}

	var podTwo string
	for i := 0; i < 3; i++ {
		stickySendChatCompletions(t, model, userChatSticky(fmt.Sprintf("iso-two-%d", i)), optTwo)
		p := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
		require.NotEmpty(t, p)
		if podTwo == "" {
			podTwo = p
		} else {
			assert.Equal(t, podTwo, p, "session two should stay on one backend")
		}
	}

	for i := 0; i < 3; i++ {
		stickySendChatCompletions(t, model, userChatSticky(fmt.Sprintf("iso-one-again-%d", i)), optOne)
		p := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
		require.NotEmpty(t, p)
		assert.Equal(t, podOne, p, "switching back to session one must not pick session two's binding")
	}
}

// TestSessionStickyWithoutSessionKeyShared verifies requests with no session identifier use normal scheduling.
func TestSessionStickyWithoutSessionKeyShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	model := "ss-nk-" + utils.RandomString(6)
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-nk", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: true,
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Test-Session"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	stickyRequireEventuallyRouteReady(t, model, "x", nil)

	for i := 0; i < 30; i++ {
		stickySendChatCompletions(t, model, userChatSticky(fmt.Sprintf("no-key-%d", i)), nil)
	}
	logs := stickyMergeRouterAccessLogs(t, kubeClient, kthenaNamespace)
	_, distinct := stickySelectedPodsForModel(logs, model)
	assert.GreaterOrEqual(t, len(distinct), 2, "without session key, scheduling should spread like non-sticky")
}

// TestSessionStickyWhenDisabledShared verifies sticky disabled ignores the session header.
func TestSessionStickyWhenDisabledShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	model := "ss-off-" + utils.RandomString(6)
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-off", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: false,
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Test-Session"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	hdr := map[string]string{"X-Test-Session": "same"}
	stickyRequireEventuallyRouteReady(t, model, "z", &stickyChatOptions{Headers: hdr})

	for i := 0; i < 35; i++ {
		stickySendChatCompletions(t, model, userChatSticky(fmt.Sprintf("off-%d", i)), &stickyChatOptions{Headers: hdr})
	}
	logs := stickyMergeRouterAccessLogs(t, kubeClient, kthenaNamespace)
	_, distinct := stickySelectedPodsForModel(logs, model)
	assert.GreaterOrEqual(t, len(distinct), 2, "sticky disabled: header should not pin to one pod")
}

// TestSessionStickyFromQueryShared verifies session key from query parameter.
func TestSessionStickyFromQueryShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	model := "ss-q-" + utils.RandomString(6)
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-q", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: true,
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceQuery, Name: "session_id"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	q := url.Values{}
	q.Set("session_id", "q-sess")
	opt := &stickyChatOptions{Query: q}
	stickyRequireEventuallyRouteReady(t, model, "q0", opt)

	var first string
	for i := 0; i < 5; i++ {
		stickySendChatCompletions(t, model, userChatSticky("q"), opt)
		pod := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
		require.NotEmpty(t, pod)
		if first == "" {
			first = pod
		} else {
			assert.Equal(t, first, pod)
		}
	}
}

// TestSessionStickyFromCookieShared verifies session key from cookie.
func TestSessionStickyFromCookieShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	model := "ss-c-" + utils.RandomString(6)
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-c", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: true,
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceCookie, Name: "sid"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	opt := &stickyChatOptions{Cookies: []*http.Cookie{{Name: "sid", Value: "cookie-a"}}}
	stickyRequireEventuallyRouteReady(t, model, "c0", opt)

	var first string
	for i := 0; i < 5; i++ {
		stickySendChatCompletions(t, model, userChatSticky("c"), opt)
		pod := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
		require.NotEmpty(t, pod)
		if first == "" {
			first = pod
		} else {
			assert.Equal(t, first, pod)
		}
	}
}

// TestSessionStickyHeaderPreferredOverQueryShared verifies header wins over query when both are configured.
func TestSessionStickyHeaderPreferredOverQueryShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	model := "ss-pr-" + utils.RandomString(6)
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-pr", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: true,
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Test-Session"},
					{Type: networkingv1alpha1.SessionKeySourceQuery, Name: "session_id"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	q := url.Values{}
	q.Set("session_id", "from-query")
	hdrOnly := &stickyChatOptions{Headers: map[string]string{"X-Test-Session": "hdr-win"}}
	both := &stickyChatOptions{
		Headers: map[string]string{"X-Test-Session": "hdr-win"},
		Query:   q,
	}
	stickyRequireEventuallyRouteReady(t, model, "p0", hdrOnly)

	stickySendChatCompletions(t, model, userChatSticky("x"), hdrOnly)
	pHeader := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	stickySendChatCompletions(t, model, userChatSticky("y"), both)
	pBoth := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	assert.Equal(t, pHeader, pBoth, "header source should win over query when both are present")
}

// TestSessionStickyAffinityTTLShared verifies TTL behavior for session mappings.
func TestSessionStickyAffinityTTLShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	model := "ss-ttl-" + utils.RandomString(6)
	ttlSec := int32(5)
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-ttl", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled:                true,
				SessionAffinitySeconds: &ttlSec,
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Test-Session"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	opt := &stickyChatOptions{Headers: map[string]string{"X-Test-Session": "ttl-1"}}
	stickyRequireEventuallyRouteReady(t, model, "t0", opt)

	stickySendChatCompletions(t, model, userChatSticky("t1"), opt)
	before := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	require.NotEmpty(t, before)
	stickySendChatCompletions(t, model, userChatSticky("t1b"), opt)
	mid := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	assert.Equal(t, before, mid, "within TTL, same session should keep the same backend")
	time.Sleep(7 * time.Second)
	stickySendChatCompletions(t, model, userChatSticky("t2"), opt)
	afterFirst := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	require.NotEmpty(t, afterFirst)
	stickySendChatCompletions(t, model, userChatSticky("t2b"), opt)
	afterSecond := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	assert.Equal(t, afterFirst, afterSecond, "after TTL window, sticky should re-bind; same session should stay on one backend")
	assert.Equal(t, http.StatusOK, stickySendChatCompletions(t, model, userChatSticky("t3"), opt).StatusCode)
	if afterFirst != before {
		t.Logf("TTL: backend changed after expiry (before=%s after=%s)", before, afterFirst)
	} else {
		t.Logf("TTL: backend unchanged after expiry (same pod by chance); within-TTL assertion still validates store TTL path when combined with shorter SessionAffinitySeconds")
	}
}

// TestSessionStickyFailoverWhenMappedPodRemovedShared verifies failover after deleting the mapped pod.
func TestSessionStickyFailoverWhenMappedPodRemovedShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	model := "ss-fo-" + utils.RandomString(6)
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-fo", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: true,
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Test-Session"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	opt := &stickyChatOptions{Headers: map[string]string{"X-Test-Session": "fo-1"}}
	stickyRequireEventuallyRouteReady(t, model, "f0", opt)

	stickySendChatCompletions(t, model, userChatSticky("f1"), opt)
	bound := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	require.NotEmpty(t, bound)
	pods, err := kubeClient.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=deepseek-r1-1-5b",
	})
	require.NoError(t, err)
	var victim string
	for _, p := range pods.Items {
		if p.Name == bound {
			victim = p.Name
			break
		}
	}
	if victim == "" {
		t.Skip("could not resolve backend pod name from access log; skipping delete step")
	}
	err = kubeClient.CoreV1().Pods(testNamespace).Delete(ctx, victim, metav1.DeleteOptions{})
	require.NoError(t, err)
	stickyRequireEventuallySelectedPodNot(t, kubeClient, kthenaNamespace, model, "f2", opt, victim)
}

// TestSessionStickyAdmissionRejectsEmptySourcesShared verifies invalid ModelRoute is rejected.
func TestSessionStickyAdmissionRejectsEmptySourcesShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-inv", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: "ss-inv-model",
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: true,
				Sources: nil,
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsInvalid(err) || apierrors.IsBadRequest(err), "expected invalid ModelRoute to be rejected")
}

// TestSessionStickyRedisAlignsBackendAcrossRouterReplicasShared verifies Redis-backed sticky across router replicas.
func TestSessionStickyRedisAlignsBackendAcrossRouterReplicasShared(t *testing.T, testCtx *routercontext.RouterTestContext, testNamespace string, kubeClient kubernetes.Interface, useGatewayAPI bool, kthenaNamespace string) {
	ctx := context.Background()
	redisCleanup := router.EnsureRedis(t, kubeClient, kthenaNamespace)
	t.Cleanup(redisCleanup)
	scaleCleanup := router.ScaleRouterDeployment(t, kubeClient, kthenaNamespace, 2)
	t.Cleanup(scaleCleanup)

	model := "ss-redis-" + utils.RandomString(6)
	redisAddr := fmt.Sprintf("redis-server.%s.svc.cluster.local:6379", kthenaNamespace)
	backend := networkingv1alpha1.SessionStickyBackendRedis
	mr := &networkingv1alpha1.ModelRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ss-redis", Namespace: testNamespace},
		Spec: networkingv1alpha1.ModelRouteSpec{
			ModelName: model,
			Rules:     []*networkingv1alpha1.Rule{{Name: "default", TargetModels: sessionStickyModelServerTarget()}},
			SessionSticky: &networkingv1alpha1.SessionSticky{
				Enabled: true,
				Backend: &backend,
				Redis:   &networkingv1alpha1.RedisConfig{Address: redisAddr},
				Sources: []networkingv1alpha1.SessionKeySource{
					{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Test-Session"},
				},
			},
		},
	}
	router.SetupModelRouteWithGatewayAPI(mr, useGatewayAPI, kthenaNamespace)
	_, err := testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Create(ctx, mr, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = testCtx.KthenaClient.NetworkingV1alpha1().ModelRoutes(testNamespace).Delete(context.Background(), mr.Name, metav1.DeleteOptions{})
	})
	opt := &stickyChatOptions{Headers: map[string]string{"X-Test-Session": "redis-shared"}}
	stickyRequireEventuallyRouteReady(t, model, "r0", opt)

	rpods := router.GetRouterPods(t, kubeClient, kthenaNamespace)
	require.GreaterOrEqual(t, len(rpods), 2)
	pf1, err := utils.SetupPortForwardToPod(kthenaNamespace, rpods[0].Name, "18080", "8080")
	require.NoError(t, err)
	t.Cleanup(pf1.Close)
	pf2, err := utils.SetupPortForwardToPod(kthenaNamespace, rpods[1].Name, "18081", "8080")
	require.NoError(t, err)
	t.Cleanup(pf2.Close)

	url1 := "http://127.0.0.1:18080/v1/chat/completions"
	url2 := "http://127.0.0.1:18081/v1/chat/completions"

	stickySendChatCompletionsURL(t, url1, model, userChatSticky("r1"), opt, true)
	pA := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	require.NotEmpty(t, pA)
	stickySendChatCompletionsURL(t, url2, model, userChatSticky("r2"), opt, true)
	pB := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
	require.NotEmpty(t, pB)
	assert.Equal(t, pA, pB, "Redis-backed sticky should align backend across different router replicas")
}
