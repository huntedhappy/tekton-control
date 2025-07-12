// File: controllers/workload_listener_handler.go
package controllers

import (
    "context"
    "fmt"
    "time"

    "k8s.io/apimachinery/pkg/api/errors"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/apimachinery/pkg/util/wait"
    "sigs.k8s.io/controller-runtime/pkg/client"
    ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
    httpProxyGroup   = "projectcontour.io"
    httpProxyVersion = "v1"
    httpProxyKind    = "HTTPProxy"

    globalProxyNS   = "argocd"
    globalProxyName = "proxy-to-listener"

    maxRetries = 5
)

func HandleHTTPProxyListener(ctx context.Context, c client.Client, workload *unstructured.Unstructured) error {
    logger := ctrlLog.FromContext(ctx)
    ns := workload.GetNamespace()
    listenerName := fmt.Sprintf("%s-listener", ns)

    if workload.GetDeletionTimestamp() != nil {
        logger.Info("Workload deleting: removing listener and global include", "listener", listenerName)

        if err := retry(func() error {
            return deleteListener(ctx, c, listenerName, ns)
        }); err != nil {
            logger.Error(err, "Failed to delete listener HTTPProxy")
            return err
        }

        if err := retry(func() error {
            return removeGlobalProxyInclude(ctx, c, listenerName, ns)
        }); err != nil {
            logger.Error(err, "Failed to remove include from global HTTPProxy")
            return err
        }

        logger.Info("Successfully cleaned up listener and global include", "listener", listenerName)
        return nil
    }

    svcName := workload.GetAnnotations()["listenerService"]
    if svcName == "" {
        svcName = "el-simple-listener"
    }

    if err := retry(func() error {
        return ensureListener(ctx, c, listenerName, ns, svcName)
    }); err != nil {
        logger.Error(err, "Failed to ensure listener HTTPProxy")
        return err
    }

    if err := retry(func() error {
        return updateGlobalProxyIncludes(ctx, c, listenerName, ns)
    }); err != nil {
        logger.Error(err, "Failed to update include in global HTTPProxy")
        return err
    }

    logger.Info("Successfully applied listener and global include", "listener", listenerName)
    return nil
}

func retry(fn func() error) error {
    backoff := wait.Backoff{
        Steps:    maxRetries,
        Duration: 200 * time.Millisecond,
        Factor:   2.0,
        Jitter:   0.1,
    }
    return wait.ExponentialBackoff(backoff, func() (bool, error) {
        err := fn()
        if errors.IsConflict(err) || errors.IsServerTimeout(err) || errors.IsTooManyRequests(err) {
            return false, nil
        }
        return err == nil, err
    })
}

func deleteListener(ctx context.Context, c client.Client, listenerName, ns string) error {
    logger := ctrlLog.FromContext(ctx)
    del := &unstructured.Unstructured{}
    del.SetGroupVersionKind(schema.GroupVersionKind{
        Group:   httpProxyGroup,
        Version: httpProxyVersion,
        Kind:    httpProxyKind,
    })
    del.SetNamespace(ns)
    del.SetName(listenerName)
    if err := c.Delete(ctx, del); err != nil && !errors.IsNotFound(err) {
        return fmt.Errorf("delete listener HTTPProxy %q: %w", listenerName, err)
    }
    logger.Info("Deleted HTTPProxy listener", "listener", listenerName)
    return nil
}

func ensureListener(ctx context.Context, c client.Client, listenerName, ns, svcName string) error {
    logger := ctrlLog.FromContext(ctx)
    proxy := &unstructured.Unstructured{}
    proxy.SetGroupVersionKind(schema.GroupVersionKind{
        Group:   httpProxyGroup,
        Version: httpProxyVersion,
        Kind:    httpProxyKind,
    })

    err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: listenerName}, proxy)
    if err != nil && !errors.IsNotFound(err) {
        return fmt.Errorf("get listener HTTPProxy %q: %w", listenerName, err)
    }

    if errors.IsNotFound(err) {
        proxy.Object = map[string]interface{}{
            "apiVersion": fmt.Sprintf("%s/%s", httpProxyGroup, httpProxyVersion),
            "kind":       httpProxyKind,
            "metadata": map[string]interface{}{
                "name":      listenerName,
                "namespace": ns,
            },
            "spec": map[string]interface{}{
                "routes": []interface{}{
                    map[string]interface{}{
                        "services": []interface{}{
                            map[string]interface{}{
                                "name": svcName,
                                "port": int64(8080),
                            },
                        },
                    },
                },
            },
        }
        if err := c.Create(ctx, proxy); err != nil && !errors.IsAlreadyExists(err) {
            return fmt.Errorf("create listener HTTPProxy %q: %w", listenerName, err)
        }
        logger.Info("Created listener HTTPProxy", "listener", listenerName)
    } else {
        logger.Info("Listener HTTPProxy already exists", "listener", listenerName)
    }
    return nil
}

func removeGlobalProxyInclude(ctx context.Context, c client.Client, listenerName, ns string) error {
    logger := ctrlLog.FromContext(ctx)
    gp := &unstructured.Unstructured{}
    gp.SetGroupVersionKind(schema.GroupVersionKind{
        Group:   httpProxyGroup,
        Version: httpProxyVersion,
        Kind:    httpProxyKind,
    })

    if err := c.Get(ctx, client.ObjectKey{Namespace: globalProxyNS, Name: globalProxyName}, gp); err != nil {
        logger.Info("🌐 Global HTTPProxy not found, skip include removal")
        return nil
    }

    incs, found, err := unstructured.NestedSlice(gp.Object, "spec", "includes")
    if err != nil || !found {
        return nil
    }

    newIncs := []interface{}{}
    removed := false
    for _, item := range incs {
        incMap, ok := item.(map[string]interface{})
        if !ok {
            continue
        }
        if incMap["name"] == listenerName && incMap["namespace"] == ns {
            removed = true
            continue
        }
        newIncs = append(newIncs, incMap)
    }

    if !removed {
        return nil
    }

    if err := unstructured.SetNestedSlice(gp.Object, newIncs, "spec", "includes"); err != nil {
        return fmt.Errorf("set includes on global proxy: %w", err)
    }
    if err := c.Update(ctx, gp); err != nil {
        return fmt.Errorf("update global proxy after include removal: %w", err)
    }

    logger.Info("✅ Removed matching includes from global HTTPProxy", "listener", listenerName)
    return nil
}

func updateGlobalProxyIncludes(ctx context.Context, c client.Client, listenerName, ns string) error {
    logger := ctrlLog.FromContext(ctx)
    gp := &unstructured.Unstructured{}
    gp.SetGroupVersionKind(schema.GroupVersionKind{
        Group:   httpProxyGroup,
        Version: httpProxyVersion,
        Kind:    httpProxyKind,
    })

    if err := c.Get(ctx, client.ObjectKey{Namespace: globalProxyNS, Name: globalProxyName}, gp); err != nil {
        return nil
    }

    oldIncs, _, _ := unstructured.NestedSlice(gp.Object, "spec", "includes")
    filtered := []interface{}{}

    // 기존 중복 제거
    for _, item := range oldIncs {
        m, ok := item.(map[string]interface{})
        if !ok {
            continue
        }
        n := m["name"]
        nsVal := m["namespace"]
        conds := m["conditions"]

        // 조건, 네임스페이스, 이름이 동일하면 제거
        if n == listenerName && nsVal == ns {
            if condList, ok := conds.([]interface{}); ok && len(condList) == 1 {
                condMap, ok := condList[0].(map[string]interface{})
                if ok && condMap["prefix"] == fmt.Sprintf("/%s", ns) {
                    continue
                }
            }
            if conds == nil {
                continue
            }
        }

        filtered = append(filtered, m)
    }

    // 새로운 include 추가
    newInclude := map[string]interface{}{
        "name":      listenerName,
        "namespace": ns,
        "conditions": []interface{}{
            map[string]interface{}{
                "prefix": fmt.Sprintf("/%s", ns),
            },
        },
    }

    filtered = append(filtered, newInclude)

    if err := unstructured.SetNestedSlice(gp.Object, filtered, "spec", "includes"); err != nil {
        return fmt.Errorf("set includes on global proxy: %w", err)
    }
    if err := c.Update(ctx, gp); err != nil {
        return fmt.Errorf("update global proxy: %w", err)
    }

    logger.Info("✅ Replaced includes in global HTTPProxy with deduplicated set", "listener", listenerName)
    return nil
}
