// File: controllers/workload_controller.go
package controllers

import (
	"context"
	"fmt"

	pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"

	tektonv1alpha1 "tekton-controller/api/v1alpha1"
	"tekton-controller/pkg/git"
	"tekton-controller/pkg/httpproxy"
	"tekton-controller/pkg/pipeline"
	"tekton-controller/pkg/util"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

const (
	workloadFinalizer = "tekton.platform/finalizer"
	TypeReady         = "Ready"
)

type WorkloadReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *WorkloadReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Workload", "namespace", req.Namespace, "name", req.Name)

	var wl tektonv1alpha1.Workload
	if err := r.Get(ctx, req.NamespacedName, &wl); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Initialize status conditions if not set
	if wl.Status.Conditions == nil || len(wl.Status.Conditions) == 0 {
		r.updateStatusCondition(ctx, &wl, metav1.ConditionUnknown, "Reconciling", "Starting reconciliation")
	}

	// Handle deletion
	if wl.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(&wl, workloadFinalizer) {
			if err := r.cleanupHTTPProxy(ctx, &wl); err != nil {
				logger.Error(err, "cleanupHTTPProxy failed")
			}
			controllerutil.RemoveFinalizer(&wl, workloadFinalizer)
			if err := r.Update(ctx, &wl); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if missing
	if !controllerutil.ContainsFinalizer(&wl, workloadFinalizer) {
		controllerutil.AddFinalizer(&wl, workloadFinalizer)
		if err := r.Update(ctx, &wl); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Ensure HTTPProxy for workload
	if err := r.ensureHTTPProxy(ctx, &wl); err != nil {
		r.updateStatusCondition(ctx, &wl, metav1.ConditionFalse, "HTTPProxyError", err.Error())
		return ctrl.Result{}, err
	}

	// Sync PipelineRun status (update Workload.Status)
	if err := r.syncPipelineRunStatus(ctx, &wl); err != nil {
		logger.Error(err, "Failed to sync PipelineRun status")
	}

	// ---------------------------
	// 생성 vs 업데이트 이벤트 구분
	// ---------------------------
	if wl.Status.LastPipelineRun == "" {
		// Workload 최초 생성 시
		wl.Status.CreateCount++ // 생성 카운트 증가
		logger.Info("Detected new Workload. Creating first PipelineRun.", "workload", wl.Name, "createCount", wl.Status.CreateCount)
		return r.createPipelineRun(ctx, &wl)
	}

	if wl.Generation > wl.Status.ObservedGeneration {
		// Workload Spec 업데이트 시
		wl.Status.UpdateCount++ // 업데이트 카운트 증가
		logger.Info("Detected Workload update. Creating new PipelineRun.", "workload", wl.Name, "updateCount", wl.Status.UpdateCount)
		return r.createPipelineRun(ctx, &wl)
	}

	// 그 외 이벤트 (상태 변경 등)
	logger.Info("No changes detected. Skipping PipelineRun creation.", "workload", wl.Name)
	return ctrl.Result{}, nil
}

func (r *WorkloadReconciler) createPipelineRun(ctx context.Context, wl *tektonv1alpha1.Workload) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Resolve latest Git SHA
	repoURL := wl.Spec.Source.Git.URL
	branch := wl.Spec.Source.Git.Ref.Branch
	if branch == "" {
		branch = "main"
	}
	auth, err := r.getGitAuth(ctx, wl.Namespace,
		util.GetAnnotationOrDefault(wl, "tekton.platform/build_git_secret", "git-credentials"))
	if err != nil {
		logger.Error(err, "Failed to get Git auth from secret")
		return ctrl.Result{}, err
	}

	resolver := git.NewResolver()
	sha, err := resolver.ResolveGitSHA(ctx, repoURL, branch, auth)
	if err != nil {
		logger.Error(err, "Failed to resolve latest Git SHA")
		return ctrl.Result{}, err
	}
	logger.Info("Resolved latest Git SHA", "branch", branch, "sha", sha)

	// Build GitInfo
	gitInfo := git.GitInfo{
		Revision: sha,
		Branch:   branch,
		URL:      repoURL,
		RepoPath: wl.Spec.Source.Git.Path,
		Name:     wl.Name,
	}

	// Extract annotations
	gitSecret := util.GetAnnotationOrDefault(wl, "tekton.platform/build_git_secret", "git-credentials")
	workspaceClaim := util.GetAnnotationOrDefault(wl, "tekton.platform/build_workspace_claim", "shared-data")

	// Create new PipelineRun
	pr, err := pipeline.NewPipelineRun(ctx, wl, gitInfo, gitSecret, workspaceClaim)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, pr); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, err
	}

	// Update Workload status
	wl.Status.LastPipelineRun = pr.Name
	wl.Status.ObservedGeneration = wl.Generation
	r.updateStatusCondition(ctx, wl, metav1.ConditionTrue, "Reconciled",
		fmt.Sprintf("PipelineRun %s created with SHA %s", pr.Name, sha))

	return ctrl.Result{}, r.Status().Update(ctx, wl)
}

func (r *WorkloadReconciler) syncPipelineRunStatus(ctx context.Context, wl *tektonv1alpha1.Workload) error {
	var prList pipelinev1beta1.PipelineRunList
	if err := r.List(ctx, &prList, client.InNamespace(wl.Namespace),
		client.MatchingLabels{"workload": wl.Name}); err != nil {
		return err
	}

	var latestPR *pipelinev1beta1.PipelineRun
	for i, pr := range prList.Items {
		if latestPR == nil || pr.CreationTimestamp.After(latestPR.CreationTimestamp.Time) {
			latestPR = &prList.Items[i]
		}
	}

	if latestPR == nil {
		return nil
	}

	if len(latestPR.Status.Conditions) > 0 {
		cond := latestPR.Status.Conditions[0]
		wl.Status.PipelineRunStatus = string(cond.Status)
		wl.Status.PipelineRunReason = cond.Reason
	}

	if latestPR.Status.StartTime != nil {
		wl.Status.LastPipelineRunStartTime = latestPR.Status.StartTime
	}

	if latestPR.Status.CompletionTime != nil {
		wl.Status.LastPipelineRunCompletionTime = latestPR.Status.CompletionTime
	}

	for _, tr := range latestPR.Status.PipelineResults {
		if tr.Name == "IMAGE_URL" {
			wl.Status.ArtifactImage = tr.Value.StringVal
		}
	}

	return r.Status().Update(ctx, wl)
}

func (r *WorkloadReconciler) getGitAuth(ctx context.Context, namespace, secretName string) (*http.BasicAuth, error) {
	if secretName == "" {
		return nil, nil
	}

	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, &secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
	}

	return git.GetGitAuthFromSecret(&secret)
}

func (r *WorkloadReconciler) ensureHTTPProxy(ctx context.Context, wl *tektonv1alpha1.Workload) error {
	u, err := util.ObjectToUnstructured(wl)
	if err != nil {
		return fmt.Errorf("convert Workload to unstructured: %w", err)
	}
	return httpproxy.HandleHTTPProxyListener(ctx, r.Client, u)
}

func (r *WorkloadReconciler) cleanupHTTPProxy(ctx context.Context, wl *tektonv1alpha1.Workload) error {
	return httpproxy.CleanupListenerForNamespace(ctx, r.Client, wl.Namespace)
}

func (r *WorkloadReconciler) updateStatusCondition(ctx context.Context, wl *tektonv1alpha1.Workload, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&wl.Status.Conditions, metav1.Condition{
		Type:    TypeReady,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
	if err := r.Status().Update(ctx, wl); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update Workload status condition")
	}
}

func (r *WorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tektonv1alpha1.Workload{}).
		Owns(&pipelinev1beta1.PipelineRun{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}). // 동시성 제한
		Complete(r)
}
