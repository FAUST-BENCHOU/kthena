/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package sessionsticky

import (
	"fmt"
	"os"
	"testing"

	"github.com/volcano-sh/kthena/test/e2e/framework"
	routercontext "github.com/volcano-sh/kthena/test/e2e/router/context"
	"github.com/volcano-sh/kthena/test/e2e/utils"
	"k8s.io/client-go/kubernetes"
)

var (
	testCtx         *routercontext.RouterTestContext
	testNamespace   string
	kthenaNamespace string
	kubeClient      *kubernetes.Clientset
)

func TestMain(m *testing.M) {
	testNamespace = "kthena-e2e-sessionsticky-" + utils.RandomString(5)
	cfg := framework.NewDefaultConfig()
	kthenaNamespace = cfg.Namespace
	cfg.NetworkingEnabled = true

	postRender, err := PostRenderScriptAbs()
	if err != nil {
		fmt.Printf("sessionsticky post-render script: %v\n", err)
		os.Exit(1)
	}
	tmp, err := os.CreateTemp("", "kthena-router-config-*.yaml")
	if err != nil {
		fmt.Printf("temp router config file: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(SessionStickyE2ERouterConfigurationYAML); err != nil {
		fmt.Printf("write router config: %v\n", err)
		os.Exit(1)
	}
	if err := tmp.Close(); err != nil {
		fmt.Printf("close temp file: %v\n", err)
		os.Exit(1)
	}
	cfg.HelmPostRendererPath = postRender
	cfg.HelmPostRendererEnv = map[string]string{
		"SESSIONSTICKY_ROUTER_CONFIG": tmp.Name(),
	}

	if err := framework.InstallKthena(cfg); err != nil {
		fmt.Printf("InstallKthena (sessionsticky post-render) failed: %v\n", err)
		os.Exit(1)
	}

	kcfg, err := utils.GetKubeConfig()
	if err != nil {
		fmt.Printf("GetKubeConfig failed: %v\n", err)
		os.Exit(1)
	}
	kubeClient, err = kubernetes.NewForConfig(kcfg)
	if err != nil {
		fmt.Printf("NewForConfig failed: %v\n", err)
		os.Exit(1)
	}

	testCtx, err = routercontext.NewRouterTestContext(testNamespace)
	if err != nil {
		fmt.Printf("NewRouterTestContext failed: %v\n", err)
		os.Exit(1)
	}
	if err := testCtx.CreateTestNamespace(); err != nil {
		fmt.Printf("CreateTestNamespace failed: %v\n", err)
		os.Exit(1)
	}
	if err := testCtx.SetupCommonComponents(); err != nil {
		fmt.Printf("SetupCommonComponents failed: %v\n", err)
		os.Exit(1)
	}
	if err := ScaleModelServerDeploymentForStickyE2E(kubeClient, testNamespace, routercontext.Deployment1_5bName, SessionStickyE2EModelServerReplicas); err != nil {
		fmt.Printf("scale model server for sessionsticky e2e: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	_ = testCtx.CleanupCommonComponents()
	_ = testCtx.DeleteTestNamespace()
	if err := framework.UninstallKthena(kthenaNamespace); err != nil {
		fmt.Printf("UninstallKthena failed: %v\n", err)
	}
	os.Exit(code)
}

func TestSessionStickyKeyPinsAndIsolates(t *testing.T) {
	TestSessionStickyKeyPinsAndIsolatesShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}

func TestSessionStickyWithoutSessionKey(t *testing.T) {
	TestSessionStickyWithoutSessionKeyShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}

func TestSessionStickyWhenDisabled(t *testing.T) {
	TestSessionStickyWhenDisabledShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}

func TestSessionStickyFromQuery(t *testing.T) {
	TestSessionStickyFromQueryShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}

func TestSessionStickyFromCookie(t *testing.T) {
	TestSessionStickyFromCookieShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}

func TestSessionStickyHeaderPreferredOverQuery(t *testing.T) {
	TestSessionStickyHeaderPreferredOverQueryShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}

func TestSessionStickyAffinityTTL(t *testing.T) {
	TestSessionStickyAffinityTTLShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}

func TestSessionStickyFailoverWhenMappedPodRemoved(t *testing.T) {
	TestSessionStickyFailoverWhenMappedPodRemovedShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}

func TestSessionStickyAdmissionRejectsEmptySources(t *testing.T) {
	TestSessionStickyAdmissionRejectsEmptySourcesShared(t, testCtx, testNamespace, false, kthenaNamespace)
}

func TestSessionStickyRedisAlignsBackendAcrossRouterReplicas(t *testing.T) {
	TestSessionStickyRedisAlignsBackendAcrossRouterReplicasShared(t, testCtx, testNamespace, kubeClient, false, kthenaNamespace)
}
