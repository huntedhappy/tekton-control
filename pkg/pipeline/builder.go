// File: pkg/pipeline/builder.go
package pipeline


import (
    "context"
    "encoding/json"
    "fmt"
    "sort"
    "time"

    apierrors "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    corev1 "k8s.io/api/core/v1"
    pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"

    "tekton-controller/pkg/util"
)

// Constants for owner reference and SA
const (
    WorkloadNameParam         = "workloadname"
    WorkloadKind              = "Workload"
    DefaultServiceAccountName = "pipeline"
)

var (
    WorkloadApiGroupVersion = schema.GroupVersion{Group: "tekton.platform", Version: "v1alpha1"}
)

// ServiceBinding represents a binding object in the CR spec.
type ServiceBinding struct {
    Name     string `json:"name"`
    Type     string `json:"type"`
    Provider string `json:"provider"`
}

// ExtractServiceBindings reads rawParams[name==key] and returns a slice of ServiceBinding.
func ExtractServiceBindings(rawParams []interface{}, key string) ([]ServiceBinding, error) {
    var result []ServiceBinding
    for _, p := range rawParams {
        pm, ok := p.(map[string]interface{})
        if !ok {
            continue
        }
        nameVal, ok := pm["name"].(string)
        if !ok || nameVal != key {
            continue
        }
        arr, _ := pm["value"].([]interface{})
        for _, item := range arr {
            m, ok := item.(map[string]interface{})
            if !ok {
                continue
            }
            sb := ServiceBinding{}
            if v, ok := m["name"].(string); ok {
                sb.Name = v
            }
            if v, ok := m["type"].(string); ok {
                sb.Type = v
            }
            if v, ok := m["provider"].(string); ok {
                sb.Provider = v
            }
            if sb.Name != "" {
                result = append(result, sb)
            }
        }
    }
    return result, nil
}

// BuildServiceBindingsJSON marshals a slice of ServiceBinding into a JSON string.
func BuildServiceBindingsJSON(sb []ServiceBinding) (string, error) {
    b, err := json.Marshal(sb)
    if err != nil {
        return "", fmt.Errorf("marshal serviceBindings: %w", err)
    }
    return string(b), nil
}

// AppendServiceBindingWorkspaces adds each binding.Name as a Secret workspace,
// but only if that workspace was declared in the pipeline spec.
func AppendServiceBindingWorkspaces(ctx context.Context, cl client.Client, ns string,
    wsDecls []pipelinev1beta1.PipelineWorkspaceDeclaration,
    currentPVC string,
    sb []ServiceBinding,
) ([]pipelinev1beta1.WorkspaceBinding, error) {
    // base PVC + secret bindings
    wsBindings, err := BuildWorkspaceBindings(ctx, cl, ns, wsDecls, currentPVC)
    if err != nil {
        return nil, err
    }
    logger := log.FromContext(ctx)

    for _, bind := range sb {
        found := false
        for _, decl := range wsDecls {
            if decl.Name == bind.Name {
                found = true
                break
            }
        }
        if !found {
            logger.V(1).Info("Skipping service-binding workspace; not declared in pipeline", "workspace", bind.Name)
            continue
        }
        logger.V(1).Info("Adding service-binding workspace", "secret", bind.Name)
        wsBindings = append(wsBindings, pipelinev1beta1.WorkspaceBinding{
            Name:   bind.Name,
            Secret: &corev1.SecretVolumeSource{SecretName: bind.Name},
        })
    }
    return wsBindings, nil
}

// ParamMapFromSpec converts a []interface{} spec params into map[name]value.
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

// BuildPipelineRunParams turns a map of params into a sorted []pipelinev1beta1.Param.
func BuildPipelineRunParams(paramsMap map[string]string) []pipelinev1beta1.Param {
    var keys []string
    for k := range paramsMap {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    var params []pipelinev1beta1.Param
    for _, k := range keys {
        params = append(params, pipelinev1beta1.Param{
            Name:  k,
            Value: *pipelinev1beta1.NewArrayOrString(paramsMap[k]),
        })
    }
    return params
}

// BuildWorkspaceBindings binds PVC and any existing Secret workspaces.
func BuildWorkspaceBindings(ctx context.Context, cl client.Client, ns string,
    pipelineWorkspaces []pipelinev1beta1.PipelineWorkspaceDeclaration,
    currentPVCClaimName string,
) ([]pipelinev1beta1.WorkspaceBinding, error) {
    logger := log.FromContext(ctx)
    var wsBindings []pipelinev1beta1.WorkspaceBinding
    for _, decl := range pipelineWorkspaces {
        wsName := decl.Name
        if util.IsPvcWorkspace(wsName) {
            wsBindings = append(wsBindings, pipelinev1beta1.WorkspaceBinding{
                Name:                  wsName,
                PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: currentPVCClaimName},
            })
        } else {
            secret := &corev1.Secret{}
            if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: wsName}, secret); err == nil {
                wsBindings = append(wsBindings, pipelinev1beta1.WorkspaceBinding{
                    Name:   wsName,
                    Secret: &corev1.SecretVolumeSource{SecretName: wsName},
                })
            } else if apierrors.IsNotFound(err) {
                logger.V(1).Info("Secret not found for workspace, skipping", "workspace", wsName)
            } else {
                logger.V(1).Info("Error fetching Secret for workspace, skipping", "workspace", wsName, "error", err)
            }
        }
    }
    return wsBindings, nil
}

// NewPipelineRun constructs a PipelineRun with owner ref, params, workspaces, etc.
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
            PipelineRef:        &pipelinev1beta1.PipelineRef{Name: pipelineName},
            Params:             params,
            Workspaces:         wsBindings,
            ServiceAccountName: DefaultServiceAccountName,
        },
    }
}

func boolPtr(b bool) *bool { return &b }

