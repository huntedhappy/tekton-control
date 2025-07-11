// File: controllers/namespace_cleanup_reconciler.go

package controllers

import (
    "context"

    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

// NamespaceCleanupReconciler는 tekton-enabled:"true" 레이블의
// 네임스페이스가 삭제되면 관련 리소스를 정리합니다.
type NamespaceCleanupReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

// SetupWithManager에서 corev1.Namespace 이벤트를 Watch하도록 설정합니다.
func (r *NamespaceCleanupReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&corev1.Namespace{}).
        Complete(r)
}

// Reconcile는 삭제된 tekton-enabled 네임스페이스만 걸러내 정리 로직을 실행합니다.
func (r *NamespaceCleanupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := ctrlLog.FromContext(ctx)
    logger.Info("Reconciling namespace cleanup", "namespace", req.Name)

    // 1) corev1.Namespace 객체 로드
    var ns corev1.Namespace
    if err := r.Get(ctx, req.NamespacedName, &ns); err != nil {
        logger.Error(err, "Failed to get Namespace")
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2) 삭제 중이 아니면 리턴
    if ns.GetDeletionTimestamp() == nil {
        logger.Info("Namespace is not being deleted, skipping", "namespace", ns.Name)
        return ctrl.Result{}, nil
    }

    // 3) tekton-enabled:"true" 레이블이 아니면 리턴
    if val := ns.Labels["tekton-enabled"]; val != "true" {
        logger.Info("Namespace is not tekton-enabled, skipping", "namespace", ns.Name)
        return ctrl.Result{}, nil
    }

    logger.Info("tekton-enabled namespace is being deleted, starting cleanup", "namespace", ns.Name)

    // 4) unstructured로 변환
    u := &unstructured.Unstructured{Object: map[string]interface{}{
        "apiVersion": "v1",
        "kind":       "Namespace",
        "metadata": map[string]interface{}{
            "name": ns.Name,
        },
    }}

    // 5) cleanup 핸들러 호출
    if err := HandleNamespaceCleanup(ctx, r.Client, u); err != nil {
        logger.Error(err, "Namespace cleanup failed", "namespace", ns.Name)
        return ctrl.Result{}, err
    }

    logger.Info("Namespace cleanup finished", "namespace", ns.Name)
    return ctrl.Result{}, nil
}
