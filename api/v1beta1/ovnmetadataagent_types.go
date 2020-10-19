/*


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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OVNMetadataAgentSpec defines the desired state of OVNMetadataAgent
type OVNMetadataAgentSpec struct {
	// name of configmap which holds general information on the OSP env
	CommonConfigMap string `json:"commonConfigMap"`
	// container image to run for the daemon
	OVNMetadataAgentImage string `json:"ovsNodeOspImage"`
	// service account used to create pods
	ServiceAccount string `json:"serviceAccount"`
	// Name of the worker role created for OSP computes
	RoleName string `json:"roleName"`
	// log level
	OvnLogLevel string `json:"ovsLogLevel"`
	// Secret containing: cell transport_url
	TransportURLSecret string `json:"transportURLSecret,omitempty"`
	//
	NovaMetadataInternal string `json:"NovaMetadataInternal,omitempty"`
	// Secret containing: metadata proxy
	NeutronMetadataProxySharedSecret string `json:"NovaMetadataInternal,omitempty"`
}

// OVNMetadataAgentStatus defines the observed state of OVNMetadataAgent
type OVNMetadataAgentStatus struct {
	// Count is the number of nodes the daemon is deployed to
	Count int32 `json:"count"`
	// Daemonset hash used to detect changes
	DaemonsetHash string `json:"daemonsetHash"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// OVNMetadataAgent is the Schema for the ovnmetadataagents API
// +kubebuilder:resource:path=ovnmetadataagent,scope=Namespaced
type OVNMetadataAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OVNMetadataAgentSpec   `json:"spec,omitempty"`
	Status OVNMetadataAgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OVNMetadataAgentList contains a list of OVNMetadataAgent
type OVNMetadataAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OVNMetadataAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OVNMetadataAgent{}, &OVNMetadataAgentList{})
}
