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
	"strings"

	"github.com/gin-gonic/gin"

	networkingv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/networking/v1alpha1"
	"github.com/volcano-sh/kthena/pkg/kthena-router/filters/auth"
)

// ExtractSessionKey returns the first non-empty session key from configured sources.
func ExtractSessionKey(c *gin.Context, spec *networkingv1alpha1.SessionSticky, authenticator *auth.JWTAuthenticator) string {
	if spec == nil || !spec.Enabled || len(spec.Sources) == 0 {
		return ""
	}
	for _, src := range spec.Sources {
		if v := extractOne(c, src, authenticator); v != "" {
			return v
		}
	}
	return ""
}

func extractOne(c *gin.Context, src networkingv1alpha1.SessionKeySource, authenticator *auth.JWTAuthenticator) string {
	name := strings.TrimSpace(src.Name)
	if name == "" {
		return ""
	}
	switch src.Type {
	case networkingv1alpha1.SessionKeySourceHeader:
		return strings.TrimSpace(c.Request.Header.Get(name))
	case networkingv1alpha1.SessionKeySourceQuery:
		return strings.TrimSpace(c.Query(name))
	case networkingv1alpha1.SessionKeySourceCookie:
		cookie, err := c.Cookie(name)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(cookie)
	case networkingv1alpha1.SessionKeySourceJWTClaim:
		if authenticator == nil {
			return ""
		}
		return authenticator.GetClaimString(c, name)
	default:
		return ""
	}
}
