// File: controllers/workload_listener_handler_test.go
package controllers

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupScheme() *runtime.Scheme {
    scheme := runtime.NewScheme()
    // 1) Workload CRD
    scheme.AddKnownTypeWithName(
        schema.GroupVersionKind{Group: "tekton.platform", Version: "v1alpha1", Kind: "Workload"},
        &unstructured.Unstructured{},
    )
    // 2) HTTPProxy CRD
    scheme.AddKnownTypeWithName(
        schema.GroupVersionKind{Group: httpProxyGroup, Version: httpProxyVersion, Kind: httpProxyKind},
        &unstructured.Unstructured{},
    )
    return scheme
}

func TestHandleHTTPProxyListener_CreatesListener(t *testing.T) {
    scheme := setupScheme()
    cli := fake.NewClientBuilder().WithScheme(scheme).Build()
    ctx := context.Background()

    // --- workload 객체를 fake client 에 미리 저장 ---
    wl := &unstructured.Unstructured{}
    wl.SetGroupVersionKind(schema.GroupVersionKind{
        Group:   "tekton.platform", Version: "v1alpha1", Kind: "Workload",
    })
    wl.SetNamespace("test-ns")
    wl.SetName("test-wl")
    wl.SetAnnotations(map[string]string{"listenerService": "my-svc"})
    assert.NoError(t, cli.Create(ctx, wl))

    // --- 실제 핸들러 호출 ---
    err := HandleHTTPProxyListener(ctx, cli, wl)
    assert.NoError(t, err)

    // --- listener HTTPProxy 생성 확인 ---
    proxy := &unstructured.Unstructured{}
    proxy.SetGroupVersionKind(schema.GroupVersionKind{
        Group: httpProxyGroup, Version: httpProxyVersion, Kind: httpProxyKind,
    })
    err = cli.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-ns-listener"}, proxy)
    assert.NoError(t, err)

    routes, found, _ := unstructured.NestedSlice(proxy.Object, "spec", "routes")
    assert.True(t, found, "routes 필드가 있어야 합니다")
    assert.Len(t, routes, 1, "routes 배열 길이는 1이어야 합니다")
}

func TestRemoveGlobalProxyInclude_RemovesElement(t *testing.T) {
    scheme := setupScheme()

    // 글로벌 proxy 객체 준비
    gp := &unstructured.Unstructured{}
    gp.SetGroupVersionKind(schema.GroupVersionKind{
        Group:   httpProxyGroup, Version: httpProxyVersion, Kind: httpProxyKind,
    })
    gp.SetNamespace(globalProxyNS)
    gp.SetName(globalProxyName)
    // spec.includes 에 테스트 항목 삽입
    _ = unstructured.SetNestedSlice(
        gp.Object,
        []interface{}{
            map[string]interface{}{"namespace": "test-ns", "name": "test-ns-listener"},
        },
        "spec", "includes",
    )

    cli := fake.NewClientBuilder().
        WithScheme(scheme).
        WithObjects(gp).
        Build()
    ctx := context.Background()

    // include 제거 호출
    err := removeGlobalProxyInclude(ctx, cli, "test-ns-listener", "test-ns")
    assert.NoError(t, err)

    // 결과 확인
    updated := &unstructured.Unstructured{}
    updated.SetGroupVersionKind(schema.GroupVersionKind{
        Group:   httpProxyGroup, Version: httpProxyVersion, Kind: httpProxyKind,
    })
    updated.SetNamespace(globalProxyNS)
    updated.SetName(globalProxyName)
    err = cli.Get(ctx, client.ObjectKey{Namespace: globalProxyNS, Name: globalProxyName}, updated)
    assert.NoError(t, err)

    inc, found, _ := unstructured.NestedSlice(updated.Object, "spec", "includes")
    assert.True(t, found, "spec.includes 필드가 있어야 합니다")
    assert.Len(t, inc, 0, "includes 배열은 제거되어 빈 상태여야 합니다")
}
