/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package sessionsticky

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// ScaleModelServerDeploymentForStickyE2E scales a Deployment and waits until ReadyReplicas match.
func ScaleModelServerDeploymentForStickyE2E(kubeClient kubernetes.Interface, namespace, deploymentName string, replicas int32) error {
	ctx := context.Background()
	d, err := kubeClient.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if d.Spec.Replicas != nil && *d.Spec.Replicas == replicas {
		return waitDeploymentReadyStickyE2E(kubeClient, namespace, deploymentName, replicas)
	}
	d.Spec.Replicas = &replicas
	if _, err := kubeClient.AppsV1().Deployments(namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return waitDeploymentReadyStickyE2E(kubeClient, namespace, deploymentName, replicas)
}

func waitDeploymentReadyStickyE2E(kubeClient kubernetes.Interface, namespace, name string, wantReplicas int32) error {
	ctx := context.Background()
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		d, err := kubeClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return d.Status.ReadyReplicas >= wantReplicas && d.Status.UpdatedReplicas >= wantReplicas, nil
	})
}
