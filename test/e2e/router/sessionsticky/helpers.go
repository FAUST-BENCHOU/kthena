/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package sessionsticky

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	networkingv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/networking/v1alpha1"
	routercontext "github.com/volcano-sh/kthena/test/e2e/router/context"
	"github.com/volcano-sh/kthena/test/e2e/utils"
)

const stickySessionHeaderLog = "[sessionsticky]"

func sessionStickyModelServerTarget() []*networkingv1alpha1.TargetModel {
	w := uint32(100)
	return []*networkingv1alpha1.TargetModel{{
		ModelServerName: routercontext.ModelServer1_5bName,
		Weight:          &w,
	}}
}

func userChatSticky(content string) []utils.ChatMessage {
	return []utils.ChatMessage{utils.NewChatMessage("user", content)}
}

// stickyChatOptions configures optional headers, cookies, and query for chat requests.
type stickyChatOptions struct {
	Headers map[string]string
	Cookies []*http.Cookie
	Query   url.Values
}

func stickySendChatCompletions(t *testing.T, modelName string, messages []utils.ChatMessage, opt *stickyChatOptions) *utils.ChatCompletionsResponse {
	t.Helper()
	return stickySendChatCompletionsURL(t, utils.DefaultRouterURL, modelName, messages, opt, true)
}

func stickySendChatCompletionsURL(t *testing.T, baseURL string, modelName string, messages []utils.ChatMessage, opt *stickyChatOptions, retry bool) *utils.ChatCompletionsResponse {
	t.Helper()
	u, err := url.Parse(baseURL)
	require.NoError(t, err)
	if opt != nil && opt.Query != nil {
		u.RawQuery = opt.Query.Encode()
	}
	body := utils.ChatCompletionsRequest{Model: modelName, Messages: messages, Stream: false}
	jsonData, err := json.Marshal(body)
	require.NoError(t, err)
	client := &http.Client{Timeout: 30 * time.Second}
	var resp *http.Response
	var responseStr string
	var attempts int
	maxAttempts := 10
	if !retry {
		maxAttempts = 1
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		attempts = attempt + 1
		req, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(jsonData))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		if opt != nil {
			for k, v := range opt.Headers {
				req.Header.Set(k, v)
			}
			for _, c := range opt.Cookies {
				req.AddCookie(c)
			}
		}
		resp, err = client.Do(req)
		if err != nil {
			fmt.Printf("%s SendChatCompletions POST attempt %d/%d url=%s err=%v\n", stickySessionHeaderLog, attempts, maxAttempts, u.String(), err)
			if retry && attempt < maxAttempts-1 {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			require.NoError(t, err)
		}
		responseBody, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, rerr)
		responseStr = string(responseBody)
		if resp.StatusCode == http.StatusOK && responseStr != "" && !strings.Contains(strings.ToLower(responseStr), "error") {
			break
		}
		if !retry {
			break
		}
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	require.NotNil(t, resp, "no HTTP response from router")
	return &utils.ChatCompletionsResponse{StatusCode: resp.StatusCode, Body: responseStr, Attempts: attempts}
}

var reStickySelectedPod = regexp.MustCompile(`selected_pod=([^\s]+)`)

func stickyMergeRouterAccessLogs(t *testing.T, kubeClient kubernetes.Interface, kthenaNamespace string) string {
	t.Helper()
	ctx := context.Background()
	pods, err := kubeClient.CoreV1().Pods(kthenaNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=kthena-router",
	})
	require.NoError(t, err)
	var lines []stickyLogLine
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		tail := int64(800)
		req := kubeClient.CoreV1().Pods(kthenaNamespace).GetLogs(p.Name, &corev1.PodLogOptions{TailLines: &tail})
		stream, err := req.Stream(ctx)
		if err != nil {
			continue
		}
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, stream)
		_ = stream.Close()
		for _, line := range strings.Split(buf.String(), "\n") {
			if ts, ok := stickyParseAccessLogTime(line); ok {
				lines = append(lines, stickyLogLine{t: ts, line: line})
			}
		}
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].t.Equal(lines[j].t) {
			return lines[i].line < lines[j].line
		}
		return lines[i].t.Before(lines[j].t)
	})
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.line)
		b.WriteByte('\n')
	}
	return b.String()
}

type stickyLogLine struct {
	t    time.Time
	line string
}

func stickyParseAccessLogTime(line string) (time.Time, bool) {
	if len(line) < 2 || line[0] != '[' {
		return time.Time{}, false
	}
	end := strings.Index(line, "]")
	if end < 1 {
		return time.Time{}, false
	}
	raw := line[1:end]
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

func stickySelectedPodsForModel(logs, modelName string) (last string, distinct map[string]struct{}) {
	distinct = make(map[string]struct{})
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, "model_name="+modelName) {
			continue
		}
		m := reStickySelectedPod.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		last = m[1]
		distinct[m[1]] = struct{}{}
	}
	return last, distinct
}

func stickyWaitAndFetchSelectedPod(t *testing.T, kubeClient kubernetes.Interface, kthenaNS, modelName string) string {
	t.Helper()
	time.Sleep(400 * time.Millisecond)
	logs := stickyMergeRouterAccessLogs(t, kubeClient, kthenaNS)
	last, _ := stickySelectedPodsForModel(logs, modelName)
	return last
}

func stickyRequireEventuallyRouteReady(t *testing.T, model, userMsg string, opt *stickyChatOptions) {
	t.Helper()
	require.Eventually(t, func() bool {
		r := stickySendChatCompletions(t, model, userChatSticky(userMsg), opt)
		return r.StatusCode == http.StatusOK
	}, 2*time.Minute, 2*time.Second, "route ready")
}

func stickyRequireEventuallySelectedPodNot(t *testing.T, kubeClient kubernetes.Interface, kthenaNamespace, model, userMsg string, opt *stickyChatOptions, previousPod string) {
	t.Helper()
	require.Eventually(t, func() bool {
		stickySendChatCompletions(t, model, userChatSticky(userMsg), opt)
		p := stickyWaitAndFetchSelectedPod(t, kubeClient, kthenaNamespace, model)
		return p != "" && p != previousPod
	}, 3*time.Minute, 3*time.Second, "after deleting mapped pod, sticky should failover to another backend")
}
