// File: pkg/pipeline/builder.go
package pipeline

import (
	"context"
	"fmt"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
    "tekton-controller/pkg/util"
    
	corev1 "k8s.io/api/core/v1"
	pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
)

const (
	WorkloadNameParam      = "workloadname"
	WorkloadKind           = "Workload"
	DefaultServiceAccountName = "pipeline"
)

var (
	WorkloadApiGroupVersion = schema.GroupVersion{Group: "tekton.platform", Version: "v1alpha1"}
)

func boolPtr(b bool) *bool { return &b }

// ParamMapFromSpec은 Workload 스펙에서 파라미터 맵을 생성합니다.
func ParamMapFromSpec(specParams []interface{}) map[string]string {
	m := make(map[string]string, len(specParams))
	for _, p := range specParams {
		if pm, ok := p.(map[string]interface{}); ok {
			nameStr, nameOk := pm["name"].(string)
			valueStr, valueOk := pm["value"].(string)
			if nameOk && valueOk {
				m[nameStr] = valueStr
			} else {
				m[fmt.Sprintf("%v", pm["name"])] = fmt.Sprintf("%v", pm["value"])
			}
		}
	}
	return m
}

// BuildPipelineRunParams는 파라미터 맵으로 Tekton 파라미터 슬라이스를 생성합니다.
func BuildPipelineRunParams(paramsMap map[string]string) []pipelinev1beta1.Param {
	var keys []string
	for k := range paramsMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var params []pipelinev1beta1.Param
	for _, k := range keys {
		params = append(params, pipelinev1beta1.Param{Name: k, Value: *pipelinev1beta1.NewArrayOrString(paramsMap[k])})
	}
	return params
}

// BuildWorkspaceBindings는 Tekton 워크스페이스 바인딩을 생성합니다.
func BuildWorkspaceBindings(ctx context.Context, cl client.Client, ns string, pipelineWorkspaces []pipelinev1beta1.PipelineWorkspaceDeclaration, currentPVCClaimName string) ([]pipelinev1beta1.WorkspaceBinding, error) {
	logger := log.FromContext(ctx)
	var wsBindings []pipelinev1beta1.WorkspaceBinding
	for _, decl := range pipelineWorkspaces {
		wsName := decl.Name
		if util.IsPvcWorkspace(wsName) {
			wsBindings = append(wsBindings, pipelinev1beta1.WorkspaceBinding{Name: wsName, PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: currentPVCClaimName}})
		} else {
			secret := &corev1.Secret{}
			if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: wsName}, secret); err == nil {
				wsBindings = append(wsBindings, pipelinev1beta1.WorkspaceBinding{Name: wsName, Secret: &corev1.SecretVolumeSource{SecretName: wsName}})
			} else if apierrors.IsNotFound(err) {
				logger.V(1).Info("Secret not found for workspace, skipping", "workspace", wsName)
			} else {
				logger.V(1).Info("Error fetching Secret for workspace, skipping", "workspace", wsName, "error", err)
			}
		}
	}
	return wsBindings, nil
}


// NewPipelineRun은 새로운 PipelineRun 객체를 생성합니다.
func NewPipelineRun(
	wl *unstructured.Unstructured,
	ns, name, pipelineName string,
	params []pipelinev1beta1.Param,
	wsBindings []pipelinev1beta1.WorkspaceBinding,
) *pipelinev1beta1.PipelineRun {
	return &pipelinev1beta1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pr-%d", name, time.Now().Unix()),
			Namespace: ns,
			Labels:    map[string]string{WorkloadNameParam: name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         fmt.Sprintf("%s/%s", WorkloadApiGroupVersion.Group, WorkloadApiGroupVersion.Version),
				Kind:               WorkloadKind,
				Name:               name,
				UID:                wl.GetUID(),
				Controller:         boolPtr(true),
				BlockOwnerDeletion: boolPtr(true),
			}},
		},
		Spec: pipelinev1beta1.PipelineRunSpec{
			PipelineRef:      &pipelinev1beta1.PipelineRef{Name: pipelineName},
			Params:           params,
			Workspaces:       wsBindings,
			ServiceAccountName: DefaultServiceAccountName,
		},
	}
}
