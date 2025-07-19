// File: pkg/util/util.go
package util

import (
	tektonv1alpha1 "tekton-controller/api/v1alpha1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// GetAnnotation returns the value of an annotation by key.
func GetAnnotation(wl *tektonv1alpha1.Workload, key string) string {
	if wl.Annotations == nil {
		return ""
	}
	return wl.Annotations[key]
}

// GetAnnotationOrDefault returns the annotation value or a default if empty.
func GetAnnotationOrDefault(wl *tektonv1alpha1.Workload, key, defaultValue string) string {
	val := GetAnnotation(wl, key)
	if val == "" {
		return defaultValue
	}
	return val
}

// GetParam returns the value of a named param from Workload.
func GetParam(wl *tektonv1alpha1.Workload, name string) string {
	for _, p := range wl.Spec.Params {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

// ObjectToUnstructured converts a runtime.Object to unstructured.Unstructured
func ObjectToUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: content}, nil
}
