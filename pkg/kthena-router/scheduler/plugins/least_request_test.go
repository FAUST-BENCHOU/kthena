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

package plugins

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/volcano-sh/kthena/pkg/kthena-router/datastore"
)

func TestLeastRequestScore(t *testing.T) {
	tests := []struct {
		name           string
		pods           []*datastore.PodInfo
		expectedScores map[string]int
	}{
		{
			name: "all pods idle",
			pods: []*datastore.PodInfo{
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}}, RequestRunningNum: 0, RequestWaitingNum: 0},
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}}, RequestRunningNum: 0, RequestWaitingNum: 0},
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3"}}, RequestRunningNum: 0, RequestWaitingNum: 0},
			},
			expectedScores: map[string]int{"pod-1": 100, "pod-2": 100, "pod-3": 100},
		},
		{
			name: "single pod idle",
			pods: []*datastore.PodInfo{
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}}, RequestRunningNum: 0, RequestWaitingNum: 0},
			},
			expectedScores: map[string]int{"pod-1": 100},
		},
		{
			name: "mixed load pods",
			pods: []*datastore.PodInfo{
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}}, RequestRunningNum: 0, RequestWaitingNum: 0},
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}}, RequestRunningNum: 10, RequestWaitingNum: 0},
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3"}}, RequestRunningNum: 5, RequestWaitingNum: 0},
			},
			expectedScores: map[string]int{"pod-1": 100, "pod-2": 0, "pod-3": 50},
		},
		{
			name: "normal non-zero case",
			pods: []*datastore.PodInfo{
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}}, RequestRunningNum: 1, RequestWaitingNum: 0},
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}}, RequestRunningNum: 2, RequestWaitingNum: 0},
				{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3"}}, RequestRunningNum: 3, RequestWaitingNum: 0},
			},
			expectedScores: map[string]int{"pod-1": 66, "pod-2": 33, "pod-3": 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := NewLeastRequest(runtime.RawExtension{Raw: []byte(`{}`)})
			scores := plugin.Score(nil, tt.pods)

			for _, pod := range tt.pods {
				podName := pod.Pod.Name
				expected := tt.expectedScores[podName]
				actual := scores[pod]
				if actual != expected {
					t.Errorf("pod %s: expected score %d, got %d", podName, expected, actual)
				}
			}
		})
	}
}
