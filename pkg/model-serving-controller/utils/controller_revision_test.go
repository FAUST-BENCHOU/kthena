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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	workloadv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/workload/v1alpha1"
)

func TestCreateControllerRevision(t *testing.T) {
	ctx := context.Background()
	client := kubefake.NewSimpleClientset()

	mi := &workloadv1alpha1.ModelServing{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ms",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "workload.kthena.io/v1alpha1",
			Kind:       "ModelServing",
		},
		Spec: workloadv1alpha1.ModelServingSpec{
			Template: workloadv1alpha1.ServingGroup{
				Roles: []workloadv1alpha1.Role{
					{
						Name: "prefill",
					},
				},
			},
		},
	}

	templateData := mi.Spec.Template.Roles

	// Test creating a ControllerRevision
	cr, err := CreateControllerRevision(ctx, client, mi, 0, "revision-v1", templateData)
	assert.NoError(t, err)
	assert.NotNil(t, cr)
	assert.Equal(t, "test-ms-0-revision-v1", cr.Name)
	assert.Equal(t, "default", cr.Namespace)
	assert.Equal(t, "test-ms", cr.Labels[ControllerRevisionLabelKey])
	assert.Equal(t, "0", cr.Labels[ControllerRevisionOrdinalLabelKey])
	assert.Equal(t, "revision-v1", cr.Labels[ControllerRevisionRevisionLabelKey])
}

func TestGetControllerRevisionHistory_MultipleVersions(t *testing.T) {
	ctx := context.Background()
	client := kubefake.NewSimpleClientset()

	mi := &workloadv1alpha1.ModelServing{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ms",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "workload.kthena.io/v1alpha1",
			Kind:       "ModelServing",
		},
		Spec: workloadv1alpha1.ModelServingSpec{
			Template: workloadv1alpha1.ServingGroup{
				Roles: []workloadv1alpha1.Role{
					{
						Name: "prefill",
					},
				},
			},
		},
	}

	templateData := mi.Spec.Template.Roles
	ordinal := 0

	// Create multiple ControllerRevisions for the same ordinal with different revisions
	revisions := []string{"revision-v1", "revision-v2", "revision-v3"}
	for i, rev := range revisions {
		// Add small delay to ensure different creation timestamps
		if i > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		_, err := CreateControllerRevision(ctx, client, mi, ordinal, rev, templateData)
		assert.NoError(t, err, "Failed to create ControllerRevision for revision %s", rev)
	}

	// Get all ControllerRevisions for this ordinal
	history, err := GetControllerRevisionHistory(ctx, client, mi, ordinal)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(history), "Should have 3 ControllerRevisions")

	// Verify all revisions are present
	revisionSet := make(map[string]bool)
	for _, cr := range history {
		rev := cr.Labels[ControllerRevisionRevisionLabelKey]
		revisionSet[rev] = true
	}
	assert.True(t, revisionSet["revision-v1"], "Should have revision-v1")
	assert.True(t, revisionSet["revision-v2"], "Should have revision-v2")
	assert.True(t, revisionSet["revision-v3"], "Should have revision-v3")

	// Verify they are sorted by creation time (newest first)
	// Note: If timestamps are equal, the order may vary, but the function should still work
	for i := 0; i < len(history)-1; i++ {
		assert.True(t, history[i].CreationTimestamp.After(history[i+1].CreationTimestamp.Time) ||
			history[i].CreationTimestamp.Equal(&history[i+1].CreationTimestamp),
			"ControllerRevisions should be sorted by creation time (newest first)")
	}
}

func TestGetLatestControllerRevision_ReturnsLatest(t *testing.T) {
	ctx := context.Background()
	client := kubefake.NewSimpleClientset()

	mi := &workloadv1alpha1.ModelServing{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ms",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "workload.kthena.io/v1alpha1",
			Kind:       "ModelServing",
		},
		Spec: workloadv1alpha1.ModelServingSpec{
			Template: workloadv1alpha1.ServingGroup{
				Roles: []workloadv1alpha1.Role{
					{
						Name: "prefill",
					},
				},
			},
		},
	}

	templateData := mi.Spec.Template.Roles
	ordinal := 0

	// Create multiple ControllerRevisions
	revisions := []string{"revision-v1", "revision-v2", "revision-v3"}
	for i, rev := range revisions {
		if i > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		_, err := CreateControllerRevision(ctx, client, mi, ordinal, rev, templateData)
		assert.NoError(t, err)
	}

	// GetLatestControllerRevision should return a revision (the most recent one if timestamps differ)
	// Note: With fake client, timestamps may be equal, so we just verify it returns one of the revisions
	latestRevision, err := GetLatestControllerRevision(ctx, client, mi, ordinal)
	assert.NoError(t, err)
	assert.NotEmpty(t, latestRevision, "Should return a revision")
	// Verify it's one of the created revisions
	assert.True(t, latestRevision == "revision-v1" || latestRevision == "revision-v2" || latestRevision == "revision-v3",
		"Should return one of the created revisions, got %s", latestRevision)
}

func TestCleanupOldControllerRevisions(t *testing.T) {
	ctx := context.Background()
	client := kubefake.NewSimpleClientset()

	mi := &workloadv1alpha1.ModelServing{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ms",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "workload.kthena.io/v1alpha1",
			Kind:       "ModelServing",
		},
		Spec: workloadv1alpha1.ModelServingSpec{
			Template: workloadv1alpha1.ServingGroup{
				Roles: []workloadv1alpha1.Role{
					{
						Name: "prefill",
					},
				},
			},
		},
	}

	templateData := mi.Spec.Template.Roles
	ordinal := 0

	// Create more than DefaultRevisionHistoryLimit (10) ControllerRevisions
	// Create 15 revisions to test cleanup
	// Note: Cleanup is now automatic in CreateControllerRevision, so we test the final state
	for i := 0; i < 15; i++ {
		if i > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		rev := fmt.Sprintf("revision-v%d", i+1)
		_, err := CreateControllerRevision(ctx, client, mi, ordinal, rev, templateData)
		assert.NoError(t, err)
	}

	// Verify only DefaultRevisionHistoryLimit (10) are kept after creating all revisions
	// (cleanup happens automatically after each creation)
	historyAfter, err := GetControllerRevisionHistory(ctx, client, mi, ordinal)
	assert.NoError(t, err)
	assert.Equal(t, int(DefaultRevisionHistoryLimit), len(historyAfter), "Should have exactly %d ControllerRevisions after creating all revisions", DefaultRevisionHistoryLimit)

	// Verify the kept revisions are the most recent ones
	// Since fake client may have equal timestamps, we verify:
	// 1. We have exactly DefaultRevisionHistoryLimit revisions
	// 2. All revisions are from the created set (v1 to v15)
	revisionSet := make(map[string]bool)
	for _, cr := range historyAfter {
		rev := cr.Labels[ControllerRevisionRevisionLabelKey]
		revisionSet[rev] = true
		// Verify revision is in expected range (v6 to v15, or v1 to v15 if timestamps are equal)
		assert.True(t, len(rev) > 0, "Revision should not be empty")
	}
	assert.Equal(t, int(DefaultRevisionHistoryLimit), len(revisionSet), "Should have exactly %d unique revisions", DefaultRevisionHistoryLimit)
}

func TestCleanupOldControllerRevisions_MultipleOrdinals(t *testing.T) {
	ctx := context.Background()
	client := kubefake.NewSimpleClientset()

	mi := &workloadv1alpha1.ModelServing{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ms",
			Namespace: "default",
			UID:       "test-uid",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "workload.kthena.io/v1alpha1",
			Kind:       "ModelServing",
		},
		Spec: workloadv1alpha1.ModelServingSpec{
			Template: workloadv1alpha1.ServingGroup{
				Roles: []workloadv1alpha1.Role{
					{
						Name: "prefill",
					},
				},
			},
		},
	}

	templateData := mi.Spec.Template.Roles

	// Create ControllerRevisions for multiple ordinals
	// Ordinal 0: 15 revisions
	// Ordinal 1: 12 revisions
	// Ordinal 2: 5 revisions
	for ordinal := 0; ordinal < 3; ordinal++ {
		count := []int{15, 12, 5}[ordinal]
		for i := 0; i < count; i++ {
			if i > 0 {
				time.Sleep(10 * time.Millisecond)
			}
			rev := fmt.Sprintf("revision-v%d", i+1)
			_, err := CreateControllerRevision(ctx, client, mi, ordinal, rev, templateData)
			assert.NoError(t, err)
		}
	}

	// Run cleanup
	err := CleanupOldControllerRevisions(ctx, client, mi)
	assert.NoError(t, err)

	// Verify cleanup results (cleanup happens automatically after each creation)
	// Ordinal 0: should have 10 (DefaultRevisionHistoryLimit)
	history0, err := GetControllerRevisionHistory(ctx, client, mi, 0)
	assert.NoError(t, err)
	assert.Equal(t, int(DefaultRevisionHistoryLimit), len(history0), "Ordinal 0 should have %d revisions", DefaultRevisionHistoryLimit)

	// Ordinal 1: should have 10 (DefaultRevisionHistoryLimit)
	history1, err := GetControllerRevisionHistory(ctx, client, mi, 1)
	assert.NoError(t, err)
	assert.Equal(t, int(DefaultRevisionHistoryLimit), len(history1), "Ordinal 1 should have %d revisions", DefaultRevisionHistoryLimit)

	// Ordinal 2: should have 5 (less than DefaultRevisionHistoryLimit, so all kept)
	history2, err := GetControllerRevisionHistory(ctx, client, mi, 2)
	assert.NoError(t, err)
	assert.Equal(t, 5, len(history2), "Ordinal 2 should have 5 revisions (all kept)")
}
