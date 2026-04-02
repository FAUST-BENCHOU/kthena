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

package sessionsticky

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	networkingv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/networking/v1alpha1"
)

func TestExtractSessionKey_Header(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Session-ID", "abc-123")

	spec := &networkingv1alpha1.SessionSticky{
		Enabled: true,
		Sources: []networkingv1alpha1.SessionKeySource{
			{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Session-ID"},
		},
	}
	got := ExtractSessionKey(c, spec, nil)
	require.Equal(t, "abc-123", got)
}

func TestExtractSessionKey_Order(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?sid=q", nil)
	c.Request.Header.Set("X-Session-ID", "from-header")

	spec := &networkingv1alpha1.SessionSticky{
		Enabled: true,
		Sources: []networkingv1alpha1.SessionKeySource{
			{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Session-ID"},
			{Type: networkingv1alpha1.SessionKeySourceQuery, Name: "sid"},
		},
	}
	got := ExtractSessionKey(c, spec, nil)
	require.Equal(t, "from-header", got)
}

func TestExtractSessionKey_Query(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?session_id=qval", nil)

	spec := &networkingv1alpha1.SessionSticky{
		Enabled: true,
		Sources: []networkingv1alpha1.SessionKeySource{
			{Type: networkingv1alpha1.SessionKeySourceQuery, Name: "session_id"},
		},
	}
	got := ExtractSessionKey(c, spec, nil)
	require.Equal(t, "qval", got)
}

func TestExtractSessionKey_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Session-ID", "x")

	spec := &networkingv1alpha1.SessionSticky{
		Enabled: false,
		Sources: []networkingv1alpha1.SessionKeySource{
			{Type: networkingv1alpha1.SessionKeySourceHeader, Name: "X-Session-ID"},
		},
	}
	require.Equal(t, "", ExtractSessionKey(c, spec, nil))
}

func TestMemoryStore_SetGet(t *testing.T) {
	s := NewMemoryStore()
	ctx := t.Context()
	require.NoError(t, s.Set(ctx, "ns/r1", "sess", "pod-a", time.Minute))
	name, ok, err := s.Get(ctx, "ns/r1", "sess")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "pod-a", name)
}
