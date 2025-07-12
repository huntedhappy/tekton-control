// File: pkg/pipeline/workspaces.go

package pipeline

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
)

// BuildWorkspaceBindings는 Workload 객체를 기반으로 PipelineRun에 필요한 워크스페이스 바인딩을 구성합니다.
func BuildWorkspaceBindings(wl *unstructured.Unstructured) []pipelinev1.WorkspaceBinding {
	// 기본 워크스페이스 리스트를 준비합니다.
	// 실제 Claim 이름이나 Secret 이름은 Workload의 annotation 등에서 가져오는 것이 좋습니다.
	workspaceBindings := []pipelinev1.WorkspaceBinding{
		{
			Name: "shared-data",
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "shared-data", 
			},
		},
		{
			Name: "git-credentials",
			Secret: &corev1.SecretVolumeSource{
				SecretName: "git-credentials",
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

				// kind가 'Secret'이고 이름이 존재하면, 워크스페이스 바인딩을 추가합니다.
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
