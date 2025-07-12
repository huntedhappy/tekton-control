// File: pkg/pipeline/builder.go

package pipeline

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
)

const (
	// 이 상수들은 pkg/util 또는 다른 공용 패키지로 옮기는 것이 더 좋습니다.
	WorkloadNameParam     = "workloadname"
	DefaultServiceAccountName = "pipeline"
)


// --- 파라미터 관련 헬퍼 함수 ---

// ParamMap은 파라미터 이름과 값을 가진 맵입니다.
type ParamMap map[string]string

// ParamMapFromSpec는 Workload의 spec.params에서 ParamMap을 생성합니다.
func ParamMapFromSpec(rawParams []interface{}) ParamMap {
	paramsMap := make(ParamMap)
	for _, rawParam := range rawParams {
		param, ok := rawParam.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := param["name"].(string)
		value, _ := param["value"].(string)
		if name != "" {
			paramsMap[name] = value
		}
	}
	return paramsMap
}

// BuildTektonParams는 ParamMap을 Tekton의 Param 슬라이스로 변환합니다.
func BuildTektonParams(params ParamMap) []pipelinev1.Param {
	// 맵 순서를 보장하기 위해 키를 정렬합니다.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	tektonParams := make([]pipelinev1.Param, 0, len(params))
	for _, k := range keys {
		tektonParams = append(tektonParams, pipelinev1.Param{
			Name: k,
			Value: pipelinev1.ParamValue{
				Type:      pipelinev1.ParamTypeString,
				StringVal: params[k],
			},
		})
	}
	return tektonParams
}


// --- 워크스페이스 관련 헬퍼 함수 ---

// BuildWorkspaceBindings는 Workload 객체를 기반으로 PipelineRun에 필요한 워크스페이스 바인딩을 구성합니다.
func BuildWorkspaceBindings(wl *unstructured.Unstructured, defaultPvcClaimName string, defaultGitSecretName string) []pipelinev1.WorkspaceBinding {
	// 기본 워크스페이스(shared-data, git-credentials)를 준비합니다.
	workspaceBindings := []pipelinev1.WorkspaceBinding{
		{
			Name: "shared-data",
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: defaultPvcClaimName,
			},
		},
		{
			Name: "git-credentials",
			Secret: &corev1.SecretVolumeSource{
				SecretName: defaultGitSecretName,
			},
		},
	}

	// Workload의 spec.params에서 'buildServiceBindings' 필드를 찾아서 처리합니다.
	bindings, found, _ := unstructured.NestedSlice(wl.Object, "spec", "params")
	if found {
		for _, param := range bindings {
			paramMap, ok := param.(map[string]interface{})
			if !ok || paramMap["name"] != "buildServiceBindings" {
				continue
			}

			value, _ := paramMap["value"].([]interface{})
			if len(value) > 0 {
				bindingMap, _ := value[0].(map[string]interface{})
				kind, _ := bindingMap["kind"].(string)
				name, _ := bindingMap["name"].(string)

				// kind가 'Secret'이고 이름이 존재하면, maven-settings 워크스페이스 바인딩을 추가합니다.
				if kind == "Secret" && name != "" {
					mavenSettingsWorkspace := pipelinev1.WorkspaceBinding{
						Name: "maven-settings", // Pipeline에 정의된 워크스페이스 이름
						Secret: &corev1.SecretVolumeSource{
							SecretName: name, // Workload에서 읽어온 Secret 이름
						},
					}
					workspaceBindings = append(workspaceBindings, mavenSettingsWorkspace)
				}
			}
			break // buildServiceBindings 처리는 한 번만
		}
	}

	return workspaceBindings
}


// --- PipelineRun 생성 헬퍼 함수 ---

// NewPipelineRun은 모든 구성요소를 조합하여 새로운 PipelineRun 객체를 생성합니다.
func NewPipelineRun(
	owner *unstructured.Unstructured,
	namespace, prName, pipelineName string,
	params []pipelinev1.Param,
	workspaces []pipelinev1.WorkspaceBinding,
) *pipelinev1.PipelineRun {
	return &pipelinev1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prName,
			Namespace: namespace,
			Labels: map[string]string{
				"tekton.platform/workload": owner.GetName(),
			},
		},
		Spec: pipelinev1.PipelineRunSpec{
			PipelineRef: &pipelinev1.PipelineRef{
				Name: pipelineName,
			},
			Params:     params,
			Workspaces: workspaces,
			PodTemplate: &pipelinev1.PodTemplate{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser: &[]int64{0}[0],
					FSGroup:   &[]int64{0}[0],
				},
			},
			ServiceAccountName: DefaultServiceAccountName,
		},
	}
}
