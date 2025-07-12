// File: controllers/workload_controller.go
package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"tekton-controller/pkg/git"
	"tekton-controller/pkg/pipeline"
	"tekton-controller/pkg/util"
)

// --- Constants (Controller-specific) ---
const (
	pipelineName                   = "master-ci-pipeline"
	finalizerName                  = "tekton.platform/workload.cleanup"
	annotationBuildGitSecret       = "tekton.platform/build-git-secret"
	annotationBuildGitToken        = "tekton.platform/build-git-token"
	annotationBuildPVCClaim        = "tekton.platform/build-workspace-claim"
	defaultGitSecretName           = "git-credentials"
	defaultPVCClaimName            = "shared-data"
	defaultImageRepoBase           = "my-registry.io"
	defaultImageRepoBasePath       = "my-project"
	requeuePermissionErrorDuration = 5 * time.Minute
	requeueGitErrorDuration        = 30 * time.Second
	requeueNotFoundDuration        = 10 * time.Second
)

// --- Unstructured Field Constants ---
const (
	specField   = "spec"
	sourceField = "source"
	gitField    = "git"
	urlField    = "url"
	refField    = "ref"
	branchField = "branch"
	paramsField = "params"
)

// --- Pipeline Parameter Name Constants ---
const (
	imageRepoAddressParam = "image-repo-address"
	imageRepoPathParam    = "image-repo-path"
	ciGitURLParam         = "ci-git-url"
	ciGitProjectNameParam = "ci-git-project-name"
	ciGitBranchParam      = "ci-git-branch"
	ciGitRevisionParam    = "ci-git-revision"
)

var (
	workloadApiGroupVersion = schema.GroupVersion{Group: "tekton.platform", Version: "v1alpha1"}
)

type WorkloadReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	GitResolver *git.Resolver
}

func (r *WorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.GitResolver = git.NewResolver()
	logger := mgr.GetLogger()
	logger.Info("Git SHA cache TTL set", "ttl", r.GitResolver.SHACacheTTL)

	return ctrl.NewControllerManagedBy(mgr).
		For(&unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": fmt.Sprintf("%s/%s", workloadApiGroupVersion.Group, workloadApiGroupVersion.Version),
			"kind":       "Workload",
		}}).
		Complete(r)
}

//+kubebuilder:rbac:groups=tekton.platform,resources=workloads,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=tekton.platform,resources=workloads/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tekton.platform,resources=workloads/finalizers,verbs=update
//+kubebuilder:rbac:groups=tekton.dev,resources=pipelines;pipelineruns,verbs=get;list;watch;create
//+kubebuilder:rbac:groups="",resources=secrets;persistentvolumeclaims,verbs=get;list;watch
//+kubebuilder:rbac:groups=projectcontour.io,resources=httpproxies,verbs=get;list;watch;create;update;patch;delete

func (r *WorkloadReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reconcileCtx, cancel, reconcileID := util.NewReconcileContext(2 * time.Minute)
	defer cancel()
	logger := log.FromContext(reconcileCtx, "reconcileID", reconcileID)

	// 1. Fetch Workload
	wl := &unstructured.Unstructured{}
	wl.SetGroupVersionKind(workloadApiGroupVersion.WithKind("Workload"))
	if err := r.Get(reconcileCtx, req.NamespacedName, wl); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get workload: %w", err)
	}
	ns, name := wl.GetNamespace(), wl.GetName()

	// 2. Handle Deletion
	if !wl.GetDeletionTimestamp().IsZero() {
		if err := HandleHTTPProxyListener(reconcileCtx, r.Client, wl); err != nil {
			return ctrl.Result{}, fmt.Errorf("cleanup failed for HTTPProxyListener: %w", err)
		}
		if util.RemoveFinalizer(wl, finalizerName) {
			if err := r.Update(reconcileCtx, wl); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
			}
			logger.Info("Workload successfully finalized.")
		}
		return ctrl.Result{}, nil
	}

	// 3. Ensure Finalizer
	if util.EnsureFinalizer(wl, finalizerName) {
		if err := r.Update(reconcileCtx, wl); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 4. Extract Git Info from Spec
	src, found, _ := unstructured.NestedMap(wl.Object, specField, sourceField, gitField)
	if !found || src == nil { return ctrl.Result{}, fmt.Errorf("git source spec not found or invalid") }
	repoURL := fmt.Sprintf("%v", src[urlField])
	refMap, _ := src[refField].(map[string]interface{})
	branch := fmt.Sprintf("%v", refMap[branchField])
	project := util.ExtractProjectName(repoURL)

	// 5. Determine Auth and Resolve Git SHA
	var auth *http.BasicAuth
	if token := util.GetAnnotationOrDefault(wl, annotationBuildGitToken, ""); token != "" {
		auth = &http.BasicAuth{Username: "oauth2", Password: token}
	} else {
		secret := &corev1.Secret{}
		gitSecretName := util.GetAnnotationOrDefault(wl, annotationBuildGitSecret, defaultGitSecretName)
		if err := r.Get(reconcileCtx, client.ObjectKey{Namespace: ns, Name: gitSecretName}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("Git secret not found, proceeding without auth", "secret", gitSecretName)
			} else {
				return ctrl.Result{}, fmt.Errorf("failed to get git secret '%s': %w", gitSecretName, err)
			}
		} else {
			var err error
			auth, err = git.GetGitAuthFromSecret(secret)
			if err != nil {
				logger.Error(err, "Failed to parse git auth from secret, retrying", "secret", gitSecretName)
				return ctrl.Result{RequeueAfter: requeueGitErrorDuration}, nil
			}
		}
	}

	sha, err := r.GitResolver.ResolveGitSHA(reconcileCtx, repoURL, branch, auth)
	if err != nil {
		logger.Error(err, "Failed to resolve Git SHA, retrying")
		return ctrl.Result{RequeueAfter: requeueGitErrorDuration}, nil
	}
	logger.Info("Successfully resolved Git SHA", "sha", sha)

	// 6. Build PipelineRun Components using Helper Functions
	prName := fmt.Sprintf("%s-pr-%d", name, wl.GetGeneration())

	params := pipeline.BuildPipelineParamsFromWorkload(
		wl, sha, repoURL, project, branch, name,
		defaultImageRepoBase, defaultImageRepoBasePath,
		defaultPVCClaimName, defaultGitSecretName,
	)

	pvcClaimName := util.GetAnnotationOrDefault(wl, annotationBuildPVCClaim, defaultPVCClaimName)
	gitSecretName := util.GetAnnotationOrDefault(wl, annotationBuildGitSecret, defaultGitSecretName)
	wsBindings := pipeline.BuildWorkspaceBindings(wl, pvcClaimName, gitSecretName)

	pr := pipeline.NewPipelineRun(wl, ns, prName, pipelineName, params, wsBindings)

	if err := ctrl.SetControllerReference(wl, pr, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set owner reference on PipelineRun: %w", err)
	}
	
	// 7. Create PipelineRun
	if err := r.Create(reconcileCtx, pr); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, fmt.Errorf("failed to create PipelineRun: %w", err)
	}
	logger.Info("PipelineRun created or already exists", "name", pr.Name)

	// 8. Update Workload's Status
	statusUpdate := map[string]interface{}{
		"lastCommitSHA":       sha,
		"lastPipelineRunName": pr.Name,
		"phase":               "Created",
	}
	if err := unstructured.SetNestedMap(wl.Object, statusUpdate, "status"); err != nil {
		logger.Error(err, "Failed to set status fields on Workload object")
		return ctrl.Result{}, err
	}
	if err := r.Status().Update(reconcileCtx, wl); err != nil {
		logger.Error(err, "Failed to update Workload status")
		return ctrl.Result{}, err
	}
    
	// 9. Handle HTTPProxy Listener
	if err := HandleHTTPProxyListener(reconcileCtx, r.Client, wl); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to handle HTTPProxy: %w", err)
	}

	logger.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}
