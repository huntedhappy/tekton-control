// File: pkg/namespace/namespace_cleanup_handler.go
package namespace

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"tekton-controller/pkg/httpproxy"

	pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	triggersv1beta1 "github.com/tektoncd/triggers/pkg/apis/triggers/v1beta1"
)

type NamespaceCleanupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *NamespaceCleanupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}

func (r *NamespaceCleanupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	nsName := req.Name

	var ns corev1.Namespace
	if err := r.Get(ctx, req.NamespacedName, &ns); err != nil {
		logger.Error(err, "Failed to get Namespace", "namespace", nsName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if ns.GetDeletionTimestamp() == nil {
		return ctrl.Result{}, nil
	}

	if val, ok := ns.Labels["tekton-enabled"]; !ok || val != "true" {
		logger.Info("Namespace is not tekton-enabled, skipping cleanup", "namespace", nsName)
		return ctrl.Result{}, nil
	}

	logger.Info("tekton-enabled namespace is being deleted, starting cleanup", "namespace", nsName)

	// Clean up Tekton resources with retry
	if err := r.cleanupTektonResourcesWithRetry(ctx, nsName); err != nil {
		logger.Error(err, "Tekton resources cleanup failed after retries", "namespace", nsName)
	}

	// Clean up HTTPProxy
	if err := httpproxy.CleanupListenerForNamespace(ctx, r.Client, nsName); err != nil {
		logger.Error(err, "Namespace HTTPProxy cleanup failed", "namespace", nsName)
	}

	logger.Info("Namespace cleanup finished", "namespace", nsName)
	return ctrl.Result{}, nil
}

func (r *NamespaceCleanupReconciler) cleanupTektonResourcesWithRetry(ctx context.Context, namespace string) error {
	logger := log.FromContext(ctx)
	deleteObjs := []client.Object{
		&pipelinev1beta1.PipelineRun{},
		&pipelinev1beta1.TaskRun{},
		&pipelinev1beta1.Pipeline{},
		&pipelinev1beta1.Task{},
		&triggersv1beta1.TriggerTemplate{},
		&triggersv1beta1.TriggerBinding{},
		&triggersv1beta1.EventListener{},
	}

	for _, obj := range deleteObjs {
		var err error
		for i := 0; i < 3; i++ { // 재시도 3회
			err = r.DeleteAllOf(ctx, obj, client.InNamespace(namespace))
			if err == nil {
				logger.Info("Deleted resources", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "namespace", namespace)
				break
			}
			logger.Error(err, "Failed to delete resource, retrying...", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "attempt", i+1)
			time.Sleep(2 * time.Second)
		}
		if err != nil {
			return fmt.Errorf("failed to delete %s after 3 retries: %w", obj.GetObjectKind().GroupVersionKind().Kind, err)
		}
	}
	return nil
}
