package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HyperfoilSpec defines the desired state of Hyperfoil
// +k8s:openapi-gen=true
type HyperfoilSpec struct {
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	Image              string `json:"image,omitempty"`
	Version            string `json:"version,omitempty"`
	Route              string `json:"route,omitempty"`
	Log                string `json:"log,omitempty"` // Name of configMap/entry
	AgentDeployTimeout int    `json:"agentDeployTimeout,omitempty"`
	TriggerURL         string `json:"triggerUrl,omitempty"`
	PreHooks           string `json:"preHooks,omitempty"`
	PostHooks          string `json:"postHooks,omitempty"`
}

// HyperfoilStatus defines the observed state of Hyperfoil
// +k8s:openapi-gen=true
type HyperfoilStatus struct {
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	Status     string      `json:"status,omitempty"`
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`
	Reason     string      `json:"reason,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Hyperfoil is the Schema for the hyperfoils API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=hyperfoils,scope=Namespaced
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Route",type=string,JSONPath=`.spec.route`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.status`
type Hyperfoil struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HyperfoilSpec   `json:"spec,omitempty"`
	Status HyperfoilStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HyperfoilList contains a list of Hyperfoil
type HyperfoilList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Hyperfoil `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Hyperfoil{}, &HyperfoilList{})
}
