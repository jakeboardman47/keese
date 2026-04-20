/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TODO(design-gate): schema defined in docs/designs/15-memory-management.md
// SharedMemorySpec is intentionally empty at v1alpha1 until the design
// gate opens. See .claude/rules/04-kubernetes.md and the plan file.
// SharedMemorySpec defines the desired state of SharedMemory.
type SharedMemorySpec struct {

}

// SharedMemoryStatus defines the observed state of SharedMemory.
type SharedMemoryStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SharedMemory is the Schema for the sharedmemories API.
type SharedMemory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SharedMemorySpec   `json:"spec,omitempty"`
	Status SharedMemoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SharedMemoryList contains a list of SharedMemory.
type SharedMemoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SharedMemory `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SharedMemory{}, &SharedMemoryList{})
}
