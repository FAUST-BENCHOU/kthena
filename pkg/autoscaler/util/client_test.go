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

package util

import (
	"context"
	"testing"
	"time"

	clientsetfake "github.com/volcano-sh/kthena/client-go/clientset/versioned/fake"
	workloadlisters "github.com/volcano-sh/kthena/client-go/listers/workload/v1alpha1"
	workload "github.com/volcano-sh/kthena/pkg/apis/workload/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

func TestGetRoleName(t *testing.T) {
	ref := &corev1.ObjectReference{Name: "role/sub"}
	role, sub, err := GetRoleName(ref)
	if err != nil {
		t.Fatalf("unexpected error")
	}
	if role != "role" || sub != "sub" {
		t.Fatalf("wrong role parsing")
	}
}

func TestGetRoleNameInvalid(t *testing.T) {
	ref := &corev1.ObjectReference{Name: "invalid"}
	_, _, err := GetRoleName(ref)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetTargetLabels(t *testing.T) {
	target := &workload.Target{}
	target.TargetRef.Name = "model1"
	target.TargetRef.Kind = workload.ModelServingKind.Kind

	selector, err := GetTargetLabels(target)
	if err != nil {
		t.Fatalf("unexpected error")
	}
	if selector == nil {
		t.Fatalf("selector is nil")
	}
}

func TestGetMetricPods(t *testing.T) {
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "default",
			Labels: map[string]string{
				workload.ModelServingNameLabelKey: "model1",
				workload.EntryLabelKey:            Entry,
			},
		},
	}

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "other-namespace",
			Labels: map[string]string{
				workload.ModelServingNameLabelKey: "model1",
				workload.EntryLabelKey:            Entry,
			},
		},
	}

	// Use k8s fake client
	kubeClient := kubefake.NewSimpleClientset(pod1, pod2)

	// Create informer factory and get lister
	informerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	podLister := informerFactory.Core().V1().Pods().Lister()

	// Start informers
	informerFactory.Start(nil)
	informerFactory.WaitForCacheSync(nil)

	target := &workload.Target{}
	target.TargetRef.Name = "model1"
	target.TargetRef.Kind = workload.ModelServingKind.Kind

	// Test filtering by "default" namespace
	pods, err := GetMetricPods(podLister, "default", target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod in default namespace, got %d", len(pods))
	}
	if pods[0].Name != "pod1" {
		t.Fatalf("expected pod1, got %s", pods[0].Name)
	}

	// Test filtering by "other-namespace"
	pods, err = GetMetricPods(podLister, "other-namespace", target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod in other-namespace, got %d", len(pods))
	}
	if pods[0].Name != "pod2" {
		t.Fatalf("expected pod2, got %s", pods[0].Name)
	}

	// Test filtering by non-existent namespace
	pods, err = GetMetricPods(podLister, "non-existent", target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 0 {
		t.Fatalf("expected 0 pods in non-existent namespace, got %d", len(pods))
	}
}

func TestUpdateModelServing(t *testing.T) {
	client := clientsetfake.NewSimpleClientset()

	model := &workload.ModelServing{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "model1",
			Namespace: "default",
		},
	}

	_, err := client.WorkloadV1alpha1().
		ModelServings("default").
		Create(context.Background(), model, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = UpdateModelServing(ctx, client, model)
	if err != nil {
		t.Fatalf("update failed")
	}
}

func TestGetModelServingTarget(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	model := &workload.ModelServing{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "model1",
			Namespace: "default",
		},
	}
	indexer.Add(model)

	lister := workloadlisters.NewModelServingLister(indexer)

	result, err := GetModelServingTarget(lister, "default", "model1")
	if err != nil {
		t.Fatalf("unexpected error")
	}
	if result.Name != "model1" {
		t.Fatalf("wrong model serving")
	}
}
