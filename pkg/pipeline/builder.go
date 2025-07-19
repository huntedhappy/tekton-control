// File: pkg/pipeline/builder.go
package pipeline

import (
	"context"

	pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tektonv1alpha1 "tekton-controller/api/v1alpha1"
	"tekton-controller/pkg/git"
	"tekton-controller/pkg/util"
)

// NewPipelineRun builds a Tekton PipelineRun from Workload.
func NewPipelineRun(ctx context.Context, wl *tektonv1alpha1.Workload, gitInfo git.GitInfo,
	gitSecret, workspaceClaim string) (*pipelinev1beta1.PipelineRun, error) {

	params := []pipelinev1beta1.Param{
		{Name: "ci-git-revision", Value: pipelinev1beta1.ParamValue{Type: pipelinev1beta1.ParamTypeString, StringVal: gitInfo.Revision}},
		{Name: "ci-git-branch", Value: pipelinev1beta1.ParamValue{Type: pipelinev1beta1.ParamTypeString, StringVal: gitInfo.Branch}},
		{Name: "ci-git-url", Value: pipelinev1beta1.ParamValue{Type: pipelinev1beta1.ParamTypeString, StringVal: gitInfo.URL}},
		{Name: "ci-git-repo-path", Value: pipelinev1beta1.ParamValue{Type: pipelinev1beta1.ParamTypeString, StringVal: gitInfo.RepoPath}},
		{Name: "ci-git-project-name", Value: pipelinev1beta1.ParamValue{Type: pipelinev1beta1.ParamTypeString, StringVal: gitInfo.Name}},
	}

	hasBuildServiceBindings := false
	serviceAccount := "pipeline" // 기본값

	for _, p := range wl.Spec.Params {
		params = append(params, pipelinev1beta1.Param{
			Name:  p.Name,
			Value: pipelinev1beta1.ParamValue{Type: pipelinev1beta1.ParamTypeString, StringVal: p.Value},
		})
		if p.Name == "buildServiceBindings" && p.Value != "" {
			hasBuildServiceBindings = true
		}
		if p.Name == "serviceAccount" && p.Value != "" {
			serviceAccount = p.Value
		}
	}

	pipelineName := "master-ci-pipeline"
	if hasBuildServiceBindings {
		pipelineName = "master-ci-pipeline-private"
	}

	var workspaces []pipelinev1beta1.WorkspaceBinding
	if workspaceClaim != "" {
		workspaceName := util.GetAnnotationOrDefault(wl, "tekton.platform/build_workspace_name", "shared-data")
		workspaces = append(workspaces, pipelinev1beta1.WorkspaceBinding{
			Name: workspaceName,
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: workspaceClaim,
			},
		})
		workspaces = append(workspaces, pipelinev1beta1.WorkspaceBinding{
			Name: "cache-data",
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "cache-data",
			},
		})
	}
	if gitSecret != "" {
		gitWorkspaceName := util.GetAnnotationOrDefault(wl, "tekton.platform/build_git_secret_name", "git-credentials")
		workspaces = append(workspaces, pipelinev1beta1.WorkspaceBinding{
			Name: gitWorkspaceName,
			Secret: &corev1.SecretVolumeSource{
				SecretName: gitSecret,
			},
		})
	}

	return &pipelinev1beta1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: wl.GetName() + "-pr-",
			Namespace:    wl.GetNamespace(),
			Labels: map[string]string{
				"workload": wl.Name, // Workload 식별 라벨 추가
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(wl, tektonv1alpha1.GroupVersion.WithKind("Workload")),
			},
		},
		Spec: pipelinev1beta1.PipelineRunSpec{
			PipelineRef:        &pipelinev1beta1.PipelineRef{Name: pipelineName},
			Params:             params,
			Workspaces:         workspaces,
			ServiceAccountName: serviceAccount, // SA 설정
		},
	}, nil
}
