/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package sessionsticky

import (
	"os"
	"strconv"
)

// ExposeBackendPodHeader returns true when the router may set X-Kthena-Backend-Pod on responses.
func ExposeBackendPodHeader() bool {
	v, ok := os.LookupEnv("ROUTER_EXPOSE_BACKEND_POD_HEADER")
	if !ok {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}
