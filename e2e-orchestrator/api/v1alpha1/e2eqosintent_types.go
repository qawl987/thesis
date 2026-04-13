/*
Copyright 2024 The Nephio Authors.

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

// IntentGroup defines a single intent group within the E2EQoSIntent.
// Each group targets specific UEs with expected QoS parameters.
type IntentGroup struct {
	// ID is a unique identifier for this intent group (e.g., "embb", "urllc").
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// Contexts defines the scope/conditions for this intent (e.g., target UEs).
	// +kubebuilder:validation:Required
	Contexts IntentContexts `json:"contexts"`

	// Expectations defines the expected QoS parameters (SLA targets).
	// +kubebuilder:validation:Required
	Expectations IntentExpectations `json:"expectations"`
}

// IntentContexts defines the context/scope of an intent group.
type IntentContexts struct {
	// TargetUEs is a list of IMSI/SUPI identifiers for target UEs.
	// +kubebuilder:validation:MinItems=1
	TargetUEs []string `json:"targetUEs"`
}

// IntentExpectations defines the expected QoS parameters for an intent group.
type IntentExpectations struct {
	// SliceType specifies the type of network slice.
	// +kubebuilder:validation:Enum=eMBB;URLLC;MIoT
	// +kubebuilder:validation:Required
	SliceType string `json:"sliceType"`

	// Latency specifies the latency requirement (for URLLC slices).
	// +kubebuilder:validation:Enum=ultra-low;low;medium
	// +optional
	Latency string `json:"latency,omitempty"`

	// Bandwidth specifies the bandwidth requirement (for eMBB slices).
	// +kubebuilder:validation:Enum=dedicated-high;high;standard
	// +optional
	Bandwidth string `json:"bandwidth,omitempty"`

	// ResourceShare specifies the PRB allocation strategy.
	// +kubebuilder:validation:Enum=Full;Partial
	// +kubebuilder:validation:Required
	ResourceShare string `json:"resourceShare"`
}

// E2EQoSIntentSpec defines the desired state of E2EQoSIntent.
type E2EQoSIntentSpec struct {
	// IntentGroups is an array of intent groups to be processed by the orchestrator.
	// +kubebuilder:validation:MinItems=1
	IntentGroups []IntentGroup `json:"intentGroups"`
}

// IntentGroupStatus represents the status of a single intent group.
// Aligned with 3GPP TS 28.312 Intent Fulfillment concepts.
type IntentGroupStatus struct {
	// ID is the intent group identifier.
	ID string `json:"id"`

	// Phase indicates the current phase of processing.
	// +kubebuilder:validation:Enum=Pending;Processing;Applied;Failed
	Phase string `json:"phase"`

	// FulfillmentState indicates whether the intent has been fulfilled per 3GPP TS 28.312.
	// +kubebuilder:validation:Enum=NOT_FULFILLED;PARTIALLY_FULFILLED;FULFILLED;DEGRADED
	// +optional
	FulfillmentState string `json:"fulfillmentState,omitempty"`

	// Message provides additional status information.
	// +optional
	Message string `json:"message,omitempty"`

	// TranslatedParams contains the translated domain-specific parameters.
	// +optional
	TranslatedParams *TranslatedParams `json:"translatedParams,omitempty"`

	// AchievedTargets reports whether each expected target has been achieved.
	// +optional
	AchievedTargets *AchievedTargets `json:"achievedTargets,omitempty"`

	// DomainStatus contains the fulfillment status of each domain (Core/RAN).
	// +optional
	DomainStatus *DomainFulfillmentStatus `json:"domainStatus,omitempty"`
}

// AchievedTargets reports whether each expected target from the intent has been achieved.
// Used for closed-loop verification per 3GPP TS 28.312.
type AchievedTargets struct {
	// Latency indicates if the latency target was achieved.
	// +kubebuilder:validation:Enum=achieved;not_achieved;not_applicable
	// +optional
	Latency string `json:"latency,omitempty"`

	// Bandwidth indicates if the bandwidth target was achieved.
	// +kubebuilder:validation:Enum=achieved;not_achieved;not_applicable
	// +optional
	Bandwidth string `json:"bandwidth,omitempty"`

	// ResourceShare indicates if the resource share allocation was applied.
	// +kubebuilder:validation:Enum=achieved;not_achieved;not_applicable
	// +optional
	ResourceShare string `json:"resourceShare,omitempty"`
}

// DomainFulfillmentStatus reports the fulfillment status for each network domain.
type DomainFulfillmentStatus struct {
	// CoreDomain indicates the fulfillment status of the Core (5GC) domain.
	// +optional
	CoreDomain *DomainStatus `json:"coreDomain,omitempty"`

	// RANDomain indicates the fulfillment status of the RAN domain.
	// +optional
	RANDomain *DomainStatus `json:"ranDomain,omitempty"`
}

// DomainStatus represents the status of a single domain.
type DomainStatus struct {
	// State indicates the current state of this domain.
	// +kubebuilder:validation:Enum=CONFIGURED;PENDING;FAILED;SKIPPED
	State string `json:"state"`

	// Message provides additional information about the domain status.
	// +optional
	Message string `json:"message,omitempty"`

	// LastUpdated indicates when this domain was last updated.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

// TranslatedParams contains the translated parameters for Core and RAN domains.
type TranslatedParams struct {
	// CoreParams contains the translated Core domain parameters.
	// +optional
	CoreParams *CoreDomainParams `json:"coreParams,omitempty"`

	// RANParams contains the translated RAN domain parameters.
	// +optional
	RANParams *RANDomainParams `json:"ranParams,omitempty"`
}

// CoreDomainParams contains the translated parameters for the Core domain (5G Core).
type CoreDomainParams struct {
	// QFI (QoS Flow Identifier) derived from 5QI mapping.
	QFI int `json:"qfi"`

	// FiveQI (5G QoS Identifier) value.
	FiveQI int `json:"fiveQI"`
}

// RANDomainParams contains the translated parameters for the RAN domain.
type RANDomainParams struct {
	// SST (Slice/Service Type).
	SST uint32 `json:"sst"`

	// SD (Slice Differentiator) in decimal.
	SD uint32 `json:"sd"`

	// MinPrbPolicyRatio is the minimum PRB policy ratio (0-100).
	MinPrbPolicyRatio uint32 `json:"minPrbPolicyRatio"`

	// MaxPrbPolicyRatio is the maximum PRB policy ratio (0-100).
	MaxPrbPolicyRatio uint32 `json:"maxPrbPolicyRatio"`

	// Priority is the scheduler priority (0-255).
	Priority uint32 `json:"priority"`
}

// E2EQoSIntentStatus defines the observed state of E2EQoSIntent.
// Aligned with 3GPP TS 28.312 Intent-driven Management closed-loop concepts.
type E2EQoSIntentStatus struct {
	// Phase indicates the overall phase of the E2EQoSIntent.
	// +kubebuilder:validation:Enum=Pending;Processing;Applied;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// FulfillmentState indicates the overall intent fulfillment state per 3GPP TS 28.312.
	// +kubebuilder:validation:Enum=NOT_FULFILLED;PARTIALLY_FULFILLED;FULFILLED;DEGRADED
	// +optional
	FulfillmentState string `json:"fulfillmentState,omitempty"`

	// ObservedGeneration is the generation of the spec that was last processed.
	// Used to detect spec changes and trigger re-reconciliation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the intent's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// IntentGroupStatuses contains the status of each intent group.
	// +optional
	IntentGroupStatuses []IntentGroupStatus `json:"intentGroupStatuses,omitempty"`

	// LastReconcileTime is the timestamp of the last reconciliation.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=e2eqos
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Fulfillment",type=string,JSONPath=`.status.fulfillmentState`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// E2EQoSIntent is the Schema for the E2E QoS Intent CRD.
// It allows rApps to declare high-level E2E network slice intents that the
// orchestrator translates into domain-specific configurations.
// Aligned with 3GPP TS 28.312 Intent-driven Management framework.
type E2EQoSIntent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   E2EQoSIntentSpec   `json:"spec,omitempty"`
	Status E2EQoSIntentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// E2EQoSIntentList contains a list of E2EQoSIntent.
type E2EQoSIntentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []E2EQoSIntent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&E2EQoSIntent{}, &E2EQoSIntentList{})
}
