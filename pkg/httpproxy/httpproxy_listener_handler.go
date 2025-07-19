// File: pkg/httpproxy/httpproxy_listener_handler.go
package httpproxy

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	HTTPProxyGroup   = "projectcontour.io"
	HTTPProxyVersion = "v1"
	HTTPProxyKind    = "HTTPProxy"
	globalProxyNS    = "argocd"
	globalProxyName  = "proxy-to-listener"
	maxRetries       = 5
)

// HandleHTTPProxyListener manages the lifecycle of an HTTPProxy listener based on workload status.
func HandleHTTPProxyListener(ctx context.Context, c client.Client, workload *unstructured.Unstructured) error {
	logger := log.FromContext(ctx)
	ns := workload.GetNamespace()
	listenerName := fmt.Sprintf("%s-listener", ns)

	// If workload is being deleted, cleanup listener and global includes
	if workload.GetDeletionTimestamp() != nil {
		logger.Info("Workload is deleting: cleaning up HTTPProxy listener and global include", "listener", listenerName)
		return CleanupListenerForNamespace(ctx, c, ns)
	}

	svcName, found := workload.GetAnnotations()["listenerService"]
	if !found || svcName == "" {
		svcName = "el-simple-listener"
	}

	logger.Info("Ensuring listener and global include are correctly configured", "listener", listenerName)

	// Ensure listener
	if err := retry(func() error {
		return ensureListener(ctx, c, listenerName, ns, svcName)
	}); err != nil {
		return fmt.Errorf("failed to ensure listener HTTPProxy: %w", err)
	}

	// Ensure global include
	if err := retry(func() error {
		return ensureGlobalProxyInclude(ctx, c, listenerName, ns)
	}); err != nil {
		return fmt.Errorf("failed to update include in global HTTPProxy: %w", err)
	}

	logger.Info("Successfully applied listener and global include", "listener", listenerName)
	return nil
}

// CleanupListenerForNamespace deletes listener HTTPProxy and removes its global include
func CleanupListenerForNamespace(ctx context.Context, c client.Client, namespace string) error {
	logger := log.FromContext(ctx)
	listenerName := fmt.Sprintf("%s-listener", namespace)
	var lastErr error

	if err := retry(func() error {
		return deleteListener(ctx, c, listenerName, namespace)
	}); err != nil {
		logger.Error(err, "Failed to delete listener HTTPProxy")
		lastErr = err
	}

	if err := retry(func() error {
		return removeGlobalProxyInclude(ctx, c, listenerName, namespace)
	}); err != nil {
		logger.Error(err, "Failed to remove include from global HTTPProxy")
		lastErr = err
	}

	if lastErr == nil {
		logger.Info("Successfully cleaned up listener and global include", "listener", listenerName)
	}
	return lastErr
}

// ensureListener creates or updates an HTTPProxy listener
func ensureListener(ctx context.Context, c client.Client, listenerName, ns, svcName string) error {
	logger := log.FromContext(ctx)
	proxy := &unstructured.Unstructured{}
	proxy.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   HTTPProxyGroup,
		Version: HTTPProxyVersion,
		Kind:    HTTPProxyKind,
	})
	proxy.SetNamespace(ns)
	proxy.SetName(listenerName)

	desiredSpec := map[string]interface{}{
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
	}

	// Check if it exists
	err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: listenerName}, proxy)
	if errors.IsNotFound(err) {
		proxy.Object["spec"] = desiredSpec
		logger.Info("Creating listener HTTPProxy", "listener", listenerName)
		return c.Create(ctx, proxy)
	} else if err != nil {
		return fmt.Errorf("get listener HTTPProxy %q: %w", listenerName, err)
	}

	// If exists, update only if svcName changed
	currentSvc, _, _ := unstructured.NestedString(proxy.Object, "spec", "routes", "0", "services", "0", "name")
	if currentSvc != svcName {
		_ = unstructured.SetNestedField(proxy.Object, svcName, "spec", "routes", "0", "services", "0", "name")
		logger.Info("Updating listener HTTPProxy with new service name", "listener", listenerName, "service", svcName)
		return c.Update(ctx, proxy)
	}
	return nil
}

func deleteListener(ctx context.Context, c client.Client, listenerName, ns string) error {
	del := &unstructured.Unstructured{}
	del.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   HTTPProxyGroup,
		Version: HTTPProxyVersion,
		Kind:    HTTPProxyKind,
	})
	del.SetNamespace(ns)
	del.SetName(listenerName)

	if err := c.Delete(ctx, del); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete listener HTTPProxy %q: %w", listenerName, err)
	}
	return nil
}

func ensureGlobalProxyInclude(ctx context.Context, c client.Client, listenerName, ns string) error {
	gp, err := getGlobalProxy(ctx, c)
	if gp == nil || err != nil {
		return err
	}

	incs, _, _ := unstructured.NestedSlice(gp.Object, "spec", "includes")
	newInclude := map[string]interface{}{
		"name":      listenerName,
		"namespace": ns,
		"conditions": []interface{}{
			map[string]interface{}{
				"prefix": fmt.Sprintf("/%s", ns),
			},
		},
	}

	// Avoid duplicates
	for _, item := range incs {
		if incMap, ok := item.(map[string]interface{}); ok &&
			incMap["name"] == listenerName && incMap["namespace"] == ns {
			return nil
		}
	}

	incs = append(incs, newInclude)
	if err := unstructured.SetNestedSlice(gp.Object, incs, "spec", "includes"); err != nil {
		return fmt.Errorf("set includes on global proxy: %w", err)
	}
	return c.Update(ctx, gp)
}

func removeGlobalProxyInclude(ctx context.Context, c client.Client, listenerName, ns string) error {
	gp, err := getGlobalProxy(ctx, c)
	if gp == nil || err != nil {
		return err
	}

	incs, found, err := unstructured.NestedSlice(gp.Object, "spec", "includes")
	if err != nil || !found {
		return nil
	}

	newIncs := []interface{}{}
	for _, item := range incs {
		if incMap, ok := item.(map[string]interface{}); ok &&
			incMap["name"] == listenerName && incMap["namespace"] == ns {
			continue
		}
		newIncs = append(newIncs, item)
	}

	if err := unstructured.SetNestedSlice(gp.Object, newIncs, "spec", "includes"); err != nil {
		return fmt.Errorf("set includes on global proxy after removal: %w", err)
	}
	return c.Update(ctx, gp)
}

func getGlobalProxy(ctx context.Context, c client.Client) (*unstructured.Unstructured, error) {
	gp := &unstructured.Unstructured{}
	gp.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   HTTPProxyGroup,
		Version: HTTPProxyVersion,
		Kind:    HTTPProxyKind,
	})
	if err := c.Get(ctx, client.ObjectKey{Namespace: globalProxyNS, Name: globalProxyName}, gp); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get global HTTPProxy: %w", err)
	}
	return gp, nil
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
		if err == nil {
			return true, nil
		}
		if errors.IsConflict(err) || errors.IsServerTimeout(err) || errors.IsTooManyRequests(err) {
			return false, nil
		}
		return false, err
	})
}
