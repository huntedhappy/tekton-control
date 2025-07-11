// File: pkg/pipeline/builder_test.go
package pipeline

import (
        "testing"
)

func TestBuildPipelineRunParams(t *testing.T) {
        // TODO: 파라미터 맵이 Tekton 파라미터 슬라이스로 올바르게 변환되는지 테스트
        // TODO: 파라미터가 이름순으로 정렬되는지 테스트
}

func TestNewPipelineRun(t *testing.T) {
        // TODO: PipelineRun 객체가 올바른 OwnerReference와 Label을 가지고 생성되는지 테스트
}
❯ cat pkg/util/annotation.go
// File: pkg/util/annotation.go
package util

import (
        "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GetAnnotationOrDefault는 오브젝트에서 어노테이션 값을 가져오거나, 없으면 기본값을 반환합니다.
func GetAnnotationOrDefault(obj *unstructured.Unstructured, key, def string) string {
        if ann := obj.GetAnnotations(); ann != nil {
                if v := ann[key]; v != "" {
                        return v
                }
        }
        return def
}

// EnsureFinalizer는 파이널라이저가 없으면 추가합니다.
func EnsureFinalizer(obj *unstructured.Unstructured, finalizer string) bool {
        for _, f := range obj.GetFinalizers() {
                if f == finalizer {
                        return false
                }
        }
        obj.SetFinalizers(append(obj.GetFinalizers(), finalizer))
        return true
}

// RemoveFinalizer는 파이널라이저가 있으면 제거합니다.
func RemoveFinalizer(obj *unstructured.Unstructured, finalizer string) bool {
        orig := obj.GetFinalizers()
        var updated []string
        removed := false
        for _, f := range orig {
                if f == finalizer {
                        removed = true
                } else {
                        updated = append(updated, f)
                }
        }
        if removed {
                obj.SetFinalizers(updated)
        }
        return removed
}
