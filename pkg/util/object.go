// File: pkg/util/object.go
package util

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GetAnnotationOrDefault는 Unstructured 객체에서 특정 Annotation 값을 조회합니다.
// Annotation이 없거나 비어있으면 지정된 기본값을 반환합니다.
func GetAnnotationOrDefault(obj *unstructured.Unstructured, annotationKey, defaultValue string) string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return defaultValue
	}
	if value, ok := annotations[annotationKey]; ok && value != "" {
		return value
	}
	return defaultValue
}

// EnsureFinalizer는 객체에 특정 파이널라이저가 없으면 추가합니다.
// 변경이 일어났을 경우 true를 반환합니다.
func EnsureFinalizer(obj *unstructured.Unstructured, finalizerName string) bool {
	finalizers := obj.GetFinalizers()
	for _, f := range finalizers {
		if f == finalizerName {
			// 이미 존재하므로 변경 없음
			return false
		}
	}
	// 존재하지 않으므로 추가
	obj.SetFinalizers(append(finalizers, finalizerName))
	return true
}

// RemoveFinalizer는 객체에서 특정 파이널라이저를 제거합니다.
// 변경이 일어났을 경우 true를 반환합니다.
func RemoveFinalizer(obj *unstructured.Unstructured, finalizerName string) bool {
	finalizers := obj.GetFinalizers()
	var newFinalizers []string
	var found bool
	for _, f := range finalizers {
		if f == finalizerName {
			found = true
			continue // 제거할 파이널라이저는 새 목록에 추가하지 않음
		}
		newFinalizers = append(newFinalizers, f)
	}

	if found {
		obj.SetFinalizers(newFinalizers)
	}

	return found
}
