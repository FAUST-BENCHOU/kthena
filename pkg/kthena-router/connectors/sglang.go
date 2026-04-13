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

package connectors

import (
	"bytes"
	"encoding/json"
	"io"
	"math/rand"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"k8s.io/klog/v2"

	"github.com/volcano-sh/kthena/pkg/kthena-router/metrics"
)

// SGLangConnector implements PD disaggregated inference for SGLang.
//
// SGLang requires a bootstrap_room (unique integer) in both the prefill and
// decode requests to coordinate the KV-cache handoff, and bootstrap_host
// (decode pod IP) in the prefill request so the prefill server knows where to
// push the KV cache.  See:
//
//	https://github.com/sgl-project/sglang/blob/main/python/sglang/srt/managers/scheduler.py
type SGLangConnector struct {
	prefillRequest *http.Request
	decodeRequest  *http.Request
	bootstrapRoom  int64
}

// NewSGLangConnector creates a new SGLang connector with a unique bootstrap room id.
func NewSGLangConnector() KVConnector {
	return &SGLangConnector{
		// rand.Int63 returns a non-negative 63-bit integer suitable for use as
		// sglang's bootstrap_room (Python int).
		bootstrapRoom: rand.Int63(),
	}
}

// Name returns the connector type name.
func (s *SGLangConnector) Name() string {
	return "sglang"
}

// Proxy executes the complete prefill-decode flow for SGLang disaggregated inference.
//
// SGLang bootstrap protocol:
//   - The prefill pod runs an HTTP bootstrap server on its disaggregation_bootstrap_port (default 8998).
//   - The decode receiver (upon receiving its request) connects to that server
//     (bootstrap_host = prefill pod IP) to exchange ZMQ endpoint metadata keyed by bootstrap_room.
//   - Only after that exchange does the prefill sender know where to push the KV cache.
//
// CRITICAL: Both the prefill and decode requests MUST be in-flight simultaneously.
// If the prefill request is dispatched first and the decode request has not yet been
// sent, no receiver will connect to the prefill's bootstrap HTTP server, causing the
// prefill to time out and abort with "KVTransferError: Aborted by AbortReq".
// The decode request carries bootstrap_host = prefillHost so the decode receiver can
// locate the prefill's bootstrap server; both requests carry the same bootstrap_room.
func (s *SGLangConnector) Proxy(c *gin.Context, reqBody map[string]interface{}, prefillAddr, decodeAddr string) (int, error) {
	// Retrieve optional metrics recorder from context.
	var metricsRecorder *metrics.RequestMetricsRecorder
	if recorder, exists := c.Get("metricsRecorder"); exists {
		if rec, ok := recorder.(*metrics.RequestMetricsRecorder); ok {
			metricsRecorder = rec
		}
	}

	// Extract prefill pod IP from "IP:PORT" — the decode receiver uses this to
	// query the prefill's bootstrap HTTP server.
	prefillHost, _, err := net.SplitHostPort(prefillAddr)
	if err != nil {
		// Fallback: use the whole address string (e.g. if port is absent).
		prefillHost = prefillAddr
	}

	req := c.Request

	// Build the decode request first (before preparePrefillBody mutates reqBody).
	// bootstrap_host = prefillHost so the decode receiver can find the prefill's
	// bootstrap server and exchange ZMQ metadata.
	if s.decodeRequest == nil {
		decodeBody := cloneReqBody(reqBody)
		decodeBody = addTokenUsage(c, decodeBody)
		decodeBody["bootstrap_room"] = s.bootstrapRoom
		decodeBody["bootstrap_host"] = prefillHost
		s.decodeRequest = s.buildDecodeRequest(c, decodeBody)
	}

	// Build the prefill request: strip streaming, cap max_tokens, add bootstrap_room.
	// The prefill sender uses bootstrap_room to track the ZMQ metadata sent by the
	// decode receiver; it does not need bootstrap_host.
	if s.prefillRequest == nil {
		prefillBody := cloneReqBody(reqBody)
		preparePrefillBody(prefillBody)
		prefillBody["bootstrap_room"] = s.bootstrapRoom
		s.prefillRequest = s.buildPrefillRequest(req, prefillBody)
	}

	// --- Launch prefill and decode concurrently ---
	// Both phases must be in-flight at the same time so that the decode receiver
	// can connect to the prefill's bootstrap HTTP server and complete the ZMQ
	// metadata exchange before the prefill tries to push the KV cache.
	if metricsRecorder != nil {
		metricsRecorder.StartPrefillPhase()
		metricsRecorder.StartDecodePhase()
		metricsRecorder.IncActiveUpstreamRequests()
		metricsRecorder.IncActiveUpstreamRequests()
	}

	type prefillOutcome struct{ err error }
	prefillCh := make(chan prefillOutcome, 1)

	go func() {
		prefillCh <- prefillOutcome{err: s.prefill(s.prefillRequest, prefillAddr)}
	}()

	// Run decode in the current goroutine so that streaming writes reach the
	// gin.Context from the request-handling goroutine.
	result, decodeErr := s.decode(c, s.decodeRequest, decodeAddr)

	// Wait for the prefill goroutine to finish before returning.
	po := <-prefillCh

	if metricsRecorder != nil {
		decodeStatus := "200"
		if decodeErr != nil {
			decodeStatus = "500"
		}
		metricsRecorder.FinishDecodePhase(decodeStatus)
		metricsRecorder.DecActiveUpstreamRequests()

		prefillStatus := "200"
		if po.err != nil {
			prefillStatus = "500"
		}
		metricsRecorder.FinishPrefillPhase(prefillStatus)
		metricsRecorder.DecActiveUpstreamRequests()
	}

	if po.err != nil {
		klog.Errorf("sglang prefill error (bootstrap_room=%d): %v", s.bootstrapRoom, po.err)
	}

	return result, decodeErr
}

func (s *SGLangConnector) prefill(req *http.Request, prefillAddr string) error {
	req.URL.Host = prefillAddr
	req.URL.Scheme = "http"
	klog.V(4).Infof("sglang prefill: sending to %s (bootstrap_room=%d)", req.URL.String(), s.bootstrapRoom)
	return prefillerProxy(nil, req)
}

func (s *SGLangConnector) decode(c *gin.Context, req *http.Request, decodeAddr string) (int, error) {
	req.URL.Host = decodeAddr
	req.URL.Scheme = "http"
	klog.V(4).Infof("sglang decode: sending to %s (bootstrap_room=%d)", req.URL.String(), s.bootstrapRoom)
	return decoderProxy(c, req)
}

func (s *SGLangConnector) buildPrefillRequest(req *http.Request, reqBody map[string]interface{}) *http.Request {
	body, err := json.Marshal(reqBody)
	if err != nil {
		klog.Errorf("sglang: failed to marshal prefill request body: %v", err)
		return nil
	}
	reqCopy := req.Clone(req.Context())
	reqCopy.URL.Scheme = "http"
	reqCopy.Body = io.NopCloser(bytes.NewBuffer(body))
	reqCopy.ContentLength = int64(len(body))
	return reqCopy
}

func (s *SGLangConnector) buildDecodeRequest(c *gin.Context, reqBody map[string]interface{}) *http.Request {
	body, err := json.Marshal(reqBody)
	if err != nil {
		klog.Errorf("sglang: failed to marshal decode request body: %v", err)
		return nil
	}
	reqCopy := c.Request.Clone(c.Request.Context())
	reqCopy.URL.Scheme = "http"
	reqCopy.Body = io.NopCloser(bytes.NewBuffer(body))
	reqCopy.ContentLength = int64(len(body))
	return reqCopy
}
