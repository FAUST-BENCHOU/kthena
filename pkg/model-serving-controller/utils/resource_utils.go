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
	workloadv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/workload/v1alpha1"
)

// ResourceWithLabels is an interface for resources that have labels
type ResourceWithLabels interface {
	GetLabels() map[string]string
	GetNamespace() string
	GetName() string
}

// GetRoleName returns the role name of the pod.
func GetRoleName(resource ResourceWithLabels) string {
	return resource.GetLabels()[workloadv1alpha1.RoleLabelKey]
}

// GetRoleID returns the role id of the pod.
func GetRoleID(resource ResourceWithLabels) string {
	return resource.GetLabels()[workloadv1alpha1.RoleIDKey]
}
