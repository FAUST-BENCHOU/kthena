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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	workloadv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/workload/v1alpha1"
)

const (
	// ControllerRevisionLabelKey is the label key for ModelServing name
	ControllerRevisionLabelKey = workloadv1alpha1.ModelServingNameLabelKey
	// ControllerRevisionOrdinalLabelKey is the label key for ordinal
	ControllerRevisionOrdinalLabelKey = "modelserving.volcano.sh/ordinal"
	// ControllerRevisionRevisionLabelKey is the label key for revision
	ControllerRevisionRevisionLabelKey = workloadv1alpha1.RevisionLabelKey
	// MaxRevisionHistory is the maximum number of ControllerRevisions to keep per ordinal
	MaxRevisionHistory = 10
)

// CreateControllerRevision creates a ControllerRevision for a specific ordinal and revision
func CreateControllerRevision(ctx context.Context, client kubernetes.Interface, mi *workloadv1alpha1.ModelServing, ordinal int, revision string, templateData interface{}) (*appsv1.ControllerRevision, error) {
	// Serialize template data
	data, err := json.Marshal(templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal template data: %v", err)
	}

	// Generate ControllerRevision name: {modelServingName}-{ordinal}-{revision}
	revisionName := fmt.Sprintf("%s-%d-%s", mi.Name, ordinal, revision)
	if len(revisionName) > 253 {
		// Kubernetes name limit is 253 characters, truncate if needed
		revisionName = revisionName[:253]
	}

	// Check if ControllerRevision already exists
	existing, err := client.AppsV1().ControllerRevisions(mi.Namespace).Get(ctx, revisionName, metav1.GetOptions{})
	if err == nil {
		// If already exists, check if data has changed
		if string(existing.Data.Raw) != string(data) {
			existing.Data = runtime.RawExtension{
				Raw: data,
			}
			existing.Revision++
			updated, updateErr := client.AppsV1().ControllerRevisions(mi.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if updateErr != nil {
				return nil, fmt.Errorf("failed to update ControllerRevision: %v", updateErr)
			}
			klog.V(4).Infof("Updated ControllerRevision %s/%s for ordinal %d with revision %s", mi.Namespace, revisionName, ordinal, revision)
			return updated, nil
		}
		return existing, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get ControllerRevision: %v", err)
	}

	// Create ControllerRevision
	cr := &appsv1.ControllerRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      revisionName,
			Namespace: mi.Namespace,
			Labels: map[string]string{
				ControllerRevisionLabelKey:         mi.Name,
				ControllerRevisionOrdinalLabelKey:  strconv.Itoa(ordinal),
				ControllerRevisionRevisionLabelKey: revision,
			},
			OwnerReferences: []metav1.OwnerReference{
				newModelServingOwnerRef(mi),
			},
		},
		Revision: 1, // ControllerRevision revision number
		Data: runtime.RawExtension{
			Raw: data,
		},
	}

	// Create ControllerRevision
	created, err := client.AppsV1().ControllerRevisions(mi.Namespace).Create(ctx, cr, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create ControllerRevision: %v", err)
	}

	klog.V(4).Infof("Created ControllerRevision %s/%s for ordinal %d with revision %s", mi.Namespace, revisionName, ordinal, revision)
	return created, nil
}

// GetControllerRevisionHistory returns all ControllerRevisions for a specific ordinal, sorted by creation time (newest first)
func GetControllerRevisionHistory(
	ctx context.Context,
	client kubernetes.Interface,
	mi *workloadv1alpha1.ModelServing,
	ordinal int,
) ([]*appsv1.ControllerRevision, error) {
	selector := labels.SelectorFromSet(map[string]string{
		ControllerRevisionLabelKey:        mi.Name,
		ControllerRevisionOrdinalLabelKey: strconv.Itoa(ordinal),
	})

	list, err := client.AppsV1().ControllerRevisions(mi.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list ControllerRevisions: %v", err)
	}

	revisions := make([]*appsv1.ControllerRevision, 0, len(list.Items))
	for i := range list.Items {
		revisions = append(revisions, &list.Items[i])
	}

	// Sort by creation time (newest first)
	sort.Slice(revisions, func(i, j int) bool {
		return revisions[i].CreationTimestamp.After(revisions[j].CreationTimestamp.Time)
	})

	return revisions, nil
}

// GetLatestControllerRevision returns the latest revision string for a specific ordinal
func GetLatestControllerRevision(ctx context.Context, client kubernetes.Interface, mi *workloadv1alpha1.ModelServing, ordinal int) (string, error) {
	revisions, err := GetControllerRevisionHistory(ctx, client, mi, ordinal)
	if err != nil {
		return "", err
	}

	if len(revisions) == 0 {
		return "", nil
	}

	// Return the revision label from the most recent ControllerRevision
	revision, ok := revisions[0].Labels[ControllerRevisionRevisionLabelKey]
	if !ok {
		return "", nil
	}

	return revision, nil
}

// CleanupOldControllerRevisions deletes old ControllerRevisions, keeping only the most recent MaxRevisionHistory per ordinal
func CleanupOldControllerRevisions(
	ctx context.Context,
	client kubernetes.Interface,
	mi *workloadv1alpha1.ModelServing,
) error {
	// Get all ControllerRevisions for this ModelServing
	selector := labels.SelectorFromSet(map[string]string{
		ControllerRevisionLabelKey: mi.Name,
	})

	list, err := client.AppsV1().ControllerRevisions(mi.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to list ControllerRevisions: %v", err)
	}

	// Group by ordinal
	ordinalRevisions := make(map[int][]*appsv1.ControllerRevision)
	for i := range list.Items {
		ordinalStr, ok := list.Items[i].Labels[ControllerRevisionOrdinalLabelKey]
		if !ok {
			continue
		}
		ordinal, err := strconv.Atoi(ordinalStr)
		if err != nil {
			klog.Warningf("Invalid ordinal in ControllerRevision %s: %s", list.Items[i].Name, ordinalStr)
			continue
		}
		ordinalRevisions[ordinal] = append(ordinalRevisions[ordinal], &list.Items[i])
	}

	// For each ordinal, keep only the most recent MaxRevisionHistory
	for ordinal, revisions := range ordinalRevisions {
		// Sort by creation time (newest first)
		sort.Slice(revisions, func(i, j int) bool {
			return revisions[i].CreationTimestamp.After(revisions[j].CreationTimestamp.Time)
		})

		// Delete old revisions
		if len(revisions) > MaxRevisionHistory {
			for i := MaxRevisionHistory; i < len(revisions); i++ {
				err := client.AppsV1().ControllerRevisions(mi.Namespace).Delete(ctx, revisions[i].Name, metav1.DeleteOptions{})
				if err != nil && !apierrors.IsNotFound(err) {
					klog.Warningf("Failed to delete old ControllerRevision %s/%s: %v", mi.Namespace, revisions[i].Name, err)
				} else {
					klog.V(4).Infof("Deleted old ControllerRevision %s/%s for ordinal %d", mi.Namespace, revisions[i].Name, ordinal)
				}
			}
		}
	}

	return nil
}
