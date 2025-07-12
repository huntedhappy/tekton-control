// File: controllers/namespace_cleanup_handler.go

package controllers

import (
    "context"
    "fmt"

    "k8s.io/apimachinery/pkg/api/errors"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "sigs.k8s.io/controller-runtime/pkg/client"
    ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

// HandleNamespaceCleanup
// - tekton-enabled:"true" 네임스페이스가 삭제되면 호출됩니다.
// - 리스너 HTTPProxy, 글로벌 include, PipelineRun을 삭제하며 로그를 남깁니다.
func HandleNamespaceCleanup(ctx context.Context, c client.Client, ns *unstructured.Unstructured) error {
    logger := ctrlLog.FromContext(ctx)
    name := ns.GetName()

    logger.Info("tekton-enabled namespace deleted, starting cleanup", "namespace", name)

    // 1) listener HTTPProxy 삭제
    listenerName := fmt.Sprintf("%s-listener", name)
    logger.Info("Deleting listener HTTPProxy", "listener", listenerName)
    proxy := &unstructured.Unstructured{}
    proxy.SetGroupVersionKind(schema.GroupVersionKind{Group: httpProxyGroup, Version: httpProxyVersion, Kind: httpProxyKind})
    proxy.SetNamespace(name)
    proxy.SetName(listenerName)
    if err := c.Delete(ctx, proxy); err != nil && !errors.IsNotFound(err) {
        logger.Error(err, "Failed to delete listener HTTPProxy", "listener", listenerName)
    } else {
        logger.Info("Deleted listener HTTPProxy", "listener", listenerName)
    }

    // 2) argocd/proxy-to-listener 글로벌 include 정리
    logger.Info("Removing include from global HTTPProxy", "listener", listenerName)
    if err := removeGlobalProxyInclude(ctx, c, listenerName, name); err != nil {
        logger.Error(err, "Failed to remove global include", "listener", listenerName)
    } else {
        logger.Info("Removed global include", "listener", listenerName)
    }

    // 3) 네임스페이스 내 모든 PipelineRun 삭제
    logger.Info("Deleting all PipelineRuns", "namespace", name)
    prList := &unstructured.UnstructuredList{}
    prList.SetGroupVersionKind(schema.GroupVersionKind{Group: "tekton.dev", Version: "v1beta1", Kind: "PipelineRunList"})
    if err := c.List(ctx, prList, client.InNamespace(name)); err != nil {
        logger.Error(err, "Failed to list PipelineRuns")
    } else {
        for _, pr := range prList.Items {
            prName := pr.GetName()
            logger.Info("Deleting PipelineRun", "name", prName)
            if err := c.Delete(ctx, &pr); err != nil && !errors.IsNotFound(err) {
                logger.Error(err, "Failed to delete PipelineRun", "name", prName)
            } else {
                logger.Info("Deleted PipelineRun", "name", prName)
            }
        }
    }

    return nil
}

