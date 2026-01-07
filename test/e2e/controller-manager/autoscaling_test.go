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
	"testing"

	workload "github.com/volcano-sh/kthena/pkg/apis/workload/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO(user): Add E2E tests for AutoscalingPolicy lifecycle (Create, Update, Delete and check if it works)
func TestAutoscalingPolicyLifecycle(t *testing.T) {
	// Placeholder for AutoscalingPolicy lifecycle tests
	t.Skip("AutoscalingPolicy lifecycle test is not implemented yet")
}

// TODO(user): Add E2E tests for AutoscalingPolicyBinding lifecycle (Create, Update, Delete and check if it works)
func TestAutoscalingPolicyBindingLifecycle(t *testing.T) {
	// Placeholder for AutoscalingPolicyBinding lifecycle tests
	t.Skip("AutoscalingPolicyBinding lifecycle test is not implemented yet")
}

func createAutoscalingPolicyWithNegativeTarget() *workload.AutoscalingPolicy {
	return &workload.AutoscalingPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "negative-target-policy",
			Namespace: testNamespace,
		},
		Spec: workload.AutoscalingPolicySpec{
			TolerancePercent: 10,
			Metrics: []workload.AutoscalingPolicyMetric{
				{
					MetricName:  "cpu",
					TargetValue: resource.MustParse("-100m"),
				},
			},
		},
	}
}

func createInvalidAutoscalingPolicy() *workload.AutoscalingPolicy {
	return &workload.AutoscalingPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-policy",
			Namespace: testNamespace,
		},
		Spec: workload.AutoscalingPolicySpec{
			TolerancePercent: 10,
			Metrics: []workload.AutoscalingPolicyMetric{
				{
					MetricName:  "cpu",
					TargetValue: resource.MustParse("100m"),
				},
				{
					MetricName:  "cpu", // duplicate metric name
					TargetValue: resource.MustParse("200m"),
				},
			},
			Behavior: workload.AutoscalingPolicyBehavior{},
		},
	}
}

func createAutoscalingPolicyWithEmptyBehavior() *workload.AutoscalingPolicy {
	return &workload.AutoscalingPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "defaulted-policy",
			Namespace: testNamespace,
		},
		Spec: workload.AutoscalingPolicySpec{
			TolerancePercent: 10,
			Metrics: []workload.AutoscalingPolicyMetric{
				{
					MetricName:  "cpu",
					TargetValue: resource.MustParse("100m"),
				},
			},
			// Behavior is empty, should be defaulted by mutator
		},
	}
}

func createTestAutoscalingPolicyBinding(policyName string) *workload.AutoscalingPolicyBinding {
	return &workload.AutoscalingPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-binding",
			Namespace: testNamespace,
		},
		Spec: workload.AutoscalingPolicyBindingSpec{
			PolicyRef: corev1.LocalObjectReference{
				Name: policyName,
			},
			HomogeneousTarget: &workload.HomogeneousTarget{
				Target: workload.Target{
					TargetRef: corev1.ObjectReference{
						Name: "some-model-serving",
						Kind: workload.ModelServingKind.Kind,
					},
				},
				MinReplicas: 1,
				MaxReplicas: 10,
			},
		},
	}
}
