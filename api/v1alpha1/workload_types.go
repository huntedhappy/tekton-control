// File: api/v1alpha1/workload_types.go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Param defines a key-value parameter for the pipeline.
type Param struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// GitRef defines Git branch/revision references.
type GitRef struct {
	Branch   string `json:"branch,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// GitConfig defines Git repository configuration.
type GitConfig struct {
	URL  string `json:"url"`
	Ref  GitRef `json:"ref,omitempty"`
	Path string `json:"path,omitempty"` // Repo path (optional)
}

// SourceConfig defines source configuration.
type SourceConfig struct {
	Git GitConfig `json:"git"`
}

// +kubebuilder:object:generate=true
type BuildConfig struct {
	Env []string `json:"env,omitempty"`
}

// +kubebuilder:object:generate=true
type ResourceConfig struct {
	Limits   map[string]string `json:"limits,omitempty"`
	Requests map[string]string `json:"requests,omitempty"`
}

// +kubebuilder:object:generate=true
type AutoScalingConfig struct {
	Enabled                           bool `json:"enabled"`
	MinReplicas                       int  `json:"minReplicas"`
	MaxReplicas                       int  `json:"maxReplicas"`
	TargetCPUUtilizationPercentage    int  `json:"targetCPUUtilizationPercentage"`
	TargetMemoryUtilizationPercentage int  `json:"targetMemoryUtilizationPercentage"`
}

// EnvConfig defines environment variables for the workload itself.
type EnvConfig struct {
	Vars []string `json:"env,omitempty"`
}

// +kubebuilder:object:generate=true
type WorkloadSpec struct {
	Source      SourceConfig      `json:"source"`
	Params      []Param           `json:"params,omitempty"`
	Build       BuildConfig       `json:"build,omitempty"`
	Env         []string          `json:"env,omitempty"`
	Resources   ResourceConfig    `json:"resources,omitempty"`
	Autoscaling AutoScalingConfig `json:"autoscaling,omitempty"`
}

// WorkloadStatus defines the observed state of Workload.
// +kubebuilder:object:generate=true
type WorkloadStatus struct {
	ObservedGeneration            int64              `json:"observedGeneration,omitempty"`
	LastPipelineRun               string             `json:"lastPipelineRun,omitempty"`
	PipelineRunStatus             string             `json:"pipelineRunStatus,omitempty"`
	PipelineRunReason             string             `json:"pipelineRunReason,omitempty"`
	LastPipelineRunStartTime      *metav1.Time       `json:"lastPipelineRunStartTime,omitempty"`
	LastPipelineRunCompletionTime *metav1.Time       `json:"lastPipelineRunCompletionTime,omitempty"`
	ArtifactImage                 string             `json:"artifactImage,omitempty"`
	CreateCount                   int64              `json:"createCount,omitempty"` // 생성 횟수
	UpdateCount                   int64              `json:"updateCount,omitempty"` // 업데이트 횟수
	Conditions                    []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Pipeline",type="string",JSONPath=".status.pipelineRunStatus"
// +kubebuilder:printcolumn:name="Image",type="string",JSONPath=".status.artifactImage"
// +kubebuilder:printcolumn:name="Created",type="integer",JSONPath=".status.createCount"
// +kubebuilder:printcolumn:name="Updated",type="integer",JSONPath=".status.updateCount"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Workload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkloadSpec   `json:"spec,omitempty"`
	Status WorkloadStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type WorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workload `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workload{}, &WorkloadList{})
}
