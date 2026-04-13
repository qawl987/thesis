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

// Package controller implements the E2E Intent Orchestrator controller.
package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	e2ev1alpha1 "e2e.intent.domain/e2e-orchestrator/api/v1alpha1"
)

// E2EQoSIntentReconciler reconciles E2EQoSIntent objects.
type E2EQoSIntentReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	PorchClient   *PorchClient
	Free5GCClient *Free5GCClient
}

// +kubebuilder:rbac:groups=e2e.intent.domain,resources=e2eqosintents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=e2e.intent.domain,resources=e2eqosintents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=e2e.intent.domain,resources=e2eqosintents/finalizers,verbs=update
// +kubebuilder:rbac:groups=porch.kpt.dev,resources=packagerevisions;packagerevisionresources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is the main control loop for E2EQoSIntent.
func (r *E2EQoSIntentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("E2EQoSIntent", req.NamespacedName)
	logger.Info("Reconcile called")

	// 1. Fetch the E2EQoSIntent object.
	intent := &e2ev1alpha1.E2EQoSIntent{}
	if err := r.Get(ctx, req.NamespacedName, intent); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("E2EQoSIntent not found; object was deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get E2EQoSIntent")
		return ctrl.Result{}, err
	}

	// Check if spec has changed by comparing generation
	// Skip only if Applied AND generation hasn't changed
	if intent.Status.Phase == "Applied" && intent.Status.ObservedGeneration == intent.Generation {
		logger.Info("Intent already applied and spec unchanged, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	// If spec changed (generation mismatch), log it
	if intent.Status.ObservedGeneration != 0 && intent.Status.ObservedGeneration != intent.Generation {
		logger.Info("Spec changed, re-processing intent",
			"previousGeneration", intent.Status.ObservedGeneration,
			"currentGeneration", intent.Generation)
	}

	// 2. Update status to Processing.
	if intent.Status.Phase != "Processing" {
		intent.Status.Phase = "Processing"
		if err := r.Status().Update(ctx, intent); err != nil {
			logger.Error(err, "Failed to update status to Processing")
			return ctrl.Result{}, err
		}
	}

	// 3. Collect all slice configs from intent groups first, then apply once to Porch.
	var intentGroupStatuses []e2ev1alpha1.IntentGroupStatus
	var allSliceConfigs []SliceConfig
	allApplied := true
	hasFailed := false

	// Phase 1: Translate all intent groups and collect RAN params
	for _, group := range intent.Spec.IntentGroups {
		groupStatus := r.translateIntentGroup(logger, &group)
		intentGroupStatuses = append(intentGroupStatuses, groupStatus)

		// Collect RAN slice config if translation succeeded
		if groupStatus.TranslatedParams != nil && groupStatus.TranslatedParams.RANParams != nil {
			ranParams := groupStatus.TranslatedParams.RANParams
			allSliceConfigs = append(allSliceConfigs, SliceConfig{
				SST:               ranParams.SST,
				SD:                ranParams.SD,
				MinPrbPolicyRatio: ranParams.MinPrbPolicyRatio,
				MaxPrbPolicyRatio: ranParams.MaxPrbPolicyRatio,
				Priority:          ranParams.Priority,
			})
		}
	}

	// Phase 1.5: Register UEs to Core domain (free5GC) via WebConsole API
	if r.Free5GCClient != nil {
		logger.Info("Registering UEs to Core domain via free5GC WebConsole")
		for i, group := range intent.Spec.IntentGroups {
			if intentGroupStatuses[i].TranslatedParams == nil {
				continue
			}
			coreParams := intentGroupStatuses[i].TranslatedParams.CoreParams
			ranParams := intentGroupStatuses[i].TranslatedParams.RANParams
			if coreParams == nil || ranParams == nil {
				continue
			}

			// Register each target UE
			coreSuccess := true
			var coreErrMsg string
			for _, ue := range group.Contexts.TargetUEs {
				logger.Info("Registering UE to free5GC", "imsi", ue, "5QI", coreParams.FiveQI, "SST", ranParams.SST, "SD", ranParams.SD)
				if err := r.Free5GCClient.RegisterSubscriber(ue, coreParams.FiveQI, ranParams.SST, ranParams.SD); err != nil {
					logger.Error(err, "Failed to register UE", "imsi", ue)
					coreSuccess = false
					coreErrMsg = err.Error()
					break
				}
			}

			// Update Core domain status
			now := metav1.Now()
			if intentGroupStatuses[i].DomainStatus == nil {
				intentGroupStatuses[i].DomainStatus = &e2ev1alpha1.DomainFulfillmentStatus{}
			}
			if coreSuccess {
				intentGroupStatuses[i].DomainStatus.CoreDomain = &e2ev1alpha1.DomainStatus{
					State:       "CONFIGURED",
					Message:     fmt.Sprintf("UEs registered with 5QI=%d", coreParams.FiveQI),
					LastUpdated: &now,
				}
			} else {
				intentGroupStatuses[i].DomainStatus.CoreDomain = &e2ev1alpha1.DomainStatus{
					State:       "FAILED",
					Message:     coreErrMsg,
					LastUpdated: &now,
				}
				hasFailed = true
			}
		}
	} else {
		logger.Info("Free5GCClient not configured, skipping Core domain UE registration")
		now := metav1.Now()
		for i := range intentGroupStatuses {
			if intentGroupStatuses[i].DomainStatus == nil {
				intentGroupStatuses[i].DomainStatus = &e2ev1alpha1.DomainFulfillmentStatus{}
			}
			intentGroupStatuses[i].DomainStatus.CoreDomain = &e2ev1alpha1.DomainStatus{
				State:       "SKIPPED",
				Message:     "Free5GCClient not configured",
				LastUpdated: &now,
			}
		}
	}

	// Phase 2: Apply ALL slices to RAN domain via single Porch workflow
	if r.PorchClient != nil && len(allSliceConfigs) > 0 {
		logger.Info("Applying all slice configs to RAN via Porch", "sliceCount", len(allSliceConfigs))
		if err := r.PorchClient.UpdateRANSliceConfigs(ctx, allSliceConfigs); err != nil {
			logger.Error(err, "Failed to update RAN slice configs via Porch")
			hasFailed = true
			// Mark all groups as failed for RAN domain
			now := metav1.Now()
			for i := range intentGroupStatuses {
				intentGroupStatuses[i].Phase = "Failed"
				intentGroupStatuses[i].FulfillmentState = "NOT_FULFILLED"
				intentGroupStatuses[i].Message = fmt.Sprintf("Failed to update RAN config: %v", err)
				if intentGroupStatuses[i].DomainStatus != nil && intentGroupStatuses[i].DomainStatus.RANDomain != nil {
					intentGroupStatuses[i].DomainStatus.RANDomain.State = "FAILED"
					intentGroupStatuses[i].DomainStatus.RANDomain.Message = err.Error()
					intentGroupStatuses[i].DomainStatus.RANDomain.LastUpdated = &now
				}
			}
		} else {
			// Mark all groups as applied
			now := metav1.Now()
			for i := range intentGroupStatuses {
				intentGroupStatuses[i].Phase = "Applied"
				intentGroupStatuses[i].FulfillmentState = "FULFILLED"
				intentGroupStatuses[i].Message = "Intent successfully translated and applied to RAN domain"
				if intentGroupStatuses[i].DomainStatus != nil && intentGroupStatuses[i].DomainStatus.RANDomain != nil {
					ranParams := intentGroupStatuses[i].TranslatedParams.RANParams
					intentGroupStatuses[i].DomainStatus.RANDomain.State = "CONFIGURED"
					intentGroupStatuses[i].DomainStatus.RANDomain.Message = fmt.Sprintf("Slice configured: SST=%d, SD=%d, maxPRB=%d", ranParams.SST, ranParams.SD, ranParams.MaxPrbPolicyRatio)
					intentGroupStatuses[i].DomainStatus.RANDomain.LastUpdated = &now
				}
				// Update achieved targets
				if intentGroupStatuses[i].AchievedTargets != nil {
					intentGroupStatuses[i].AchievedTargets.ResourceShare = "achieved"
					group := intent.Spec.IntentGroups[i]
					if group.Expectations.SliceType == "URLLC" {
						intentGroupStatuses[i].AchievedTargets.Latency = "achieved"
					} else if group.Expectations.SliceType == "eMBB" {
						intentGroupStatuses[i].AchievedTargets.Bandwidth = "achieved"
					}
				}
			}
		}
	} else if r.PorchClient == nil {
		logger.Info("PorchClient not configured, skipping RAN domain update")
		for i := range intentGroupStatuses {
			intentGroupStatuses[i].Phase = "Applied"
			intentGroupStatuses[i].FulfillmentState = "FULFILLED"
			if intentGroupStatuses[i].DomainStatus != nil && intentGroupStatuses[i].DomainStatus.RANDomain != nil {
				intentGroupStatuses[i].DomainStatus.RANDomain.State = "SKIPPED"
				intentGroupStatuses[i].DomainStatus.RANDomain.Message = "PorchClient not configured"
			}
		}
	}

	// Check final status
	for _, gs := range intentGroupStatuses {
		if gs.Phase != "Applied" {
			allApplied = false
		}
		if gs.Phase == "Failed" {
			hasFailed = true
		}
	}

	// 4. Update the overall status.
	intent.Status.IntentGroupStatuses = intentGroupStatuses
	intent.Status.ObservedGeneration = intent.Generation // Track which generation we processed
	now := metav1.Now()
	intent.Status.LastReconcileTime = &now

	if allApplied {
		intent.Status.Phase = "Applied"
		intent.Status.FulfillmentState = "FULFILLED" // 3GPP TS 28.312 closed-loop
		intent.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "AllIntentsApplied",
				Message:            "All intent groups have been successfully translated and applied",
				LastTransitionTime: now,
			},
		}
	} else if hasFailed {
		// Mark as Failed and don't retry automatically
		intent.Status.Phase = "Failed"
		intent.Status.FulfillmentState = "NOT_FULFILLED" // 3GPP TS 28.312 closed-loop
		intent.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "IntentsFailed",
				Message:            "One or more intent groups failed to apply",
				LastTransitionTime: now,
			},
		}
	} else {
		intent.Status.Phase = "Processing"
		intent.Status.FulfillmentState = "PARTIALLY_FULFILLED" // 3GPP TS 28.312 closed-loop
		intent.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "IntentsProcessing",
				Message:            "Some intent groups are still being processed",
				LastTransitionTime: now,
			},
		}
	}

	if err := r.Status().Update(ctx, intent); err != nil {
		logger.Error(err, "Failed to update E2EQoSIntent status")
		return ctrl.Result{}, err
	}

	// Don't requeue on failure or success - only requeue if still processing
	// and not failed (to avoid infinite retry loops)
	if !allApplied && !hasFailed {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// translateIntentGroup translates a single intent group to domain parameters.
// Does NOT apply to Porch - that's done in bulk in Reconcile.
func (r *E2EQoSIntentReconciler) translateIntentGroup(logger logr.Logger, group *e2ev1alpha1.IntentGroup) e2ev1alpha1.IntentGroupStatus {
	logger = logger.WithValues("intentGroup", group.ID)
	logger.Info("Translating intent group")

	now := metav1.Now()
	status := e2ev1alpha1.IntentGroupStatus{
		ID:               group.ID,
		Phase:            "Processing",
		FulfillmentState: "NOT_FULFILLED",
	}

	// Initialize domain status
	status.DomainStatus = &e2ev1alpha1.DomainFulfillmentStatus{
		CoreDomain: &e2ev1alpha1.DomainStatus{
			State:       "SKIPPED",
			Message:     "UEs already registered manually",
			LastUpdated: &now,
		},
		RANDomain: &e2ev1alpha1.DomainStatus{
			State:       "PENDING",
			LastUpdated: &now,
		},
	}

	// Initialize achieved targets based on slice type
	status.AchievedTargets = &e2ev1alpha1.AchievedTargets{
		ResourceShare: "not_achieved",
	}
	if group.Expectations.SliceType == "URLLC" {
		status.AchievedTargets.Latency = "not_achieved"
		status.AchievedTargets.Bandwidth = "not_applicable"
	} else if group.Expectations.SliceType == "eMBB" {
		status.AchievedTargets.Latency = "not_applicable"
		status.AchievedTargets.Bandwidth = "not_achieved"
	}

	// Translate intent to domain-specific parameters
	coreParams := translateToCoreParams(group)
	ranParams := translateToRANParams(group)

	status.TranslatedParams = &e2ev1alpha1.TranslatedParams{
		CoreParams: coreParams,
		RANParams:  ranParams,
	}

	logger.Info("Translated intent parameters",
		"coreParams", fmt.Sprintf("5QI=%d, QFI=%d", coreParams.FiveQI, coreParams.QFI),
		"ranParams", fmt.Sprintf("SST=%d, SD=%d, minPRB=%d, maxPRB=%d, priority=%d",
			ranParams.SST, ranParams.SD, ranParams.MinPrbPolicyRatio, ranParams.MaxPrbPolicyRatio, ranParams.Priority))

	return status
}

// translateToCoreParams translates intent expectations to Core domain parameters.
// Uses simple switch/case mapping as specified.
func translateToCoreParams(group *e2ev1alpha1.IntentGroup) *e2ev1alpha1.CoreDomainParams {
	var fiveQI int

	switch group.Expectations.SliceType {
	case "URLLC":
		// Map latency to 5QI for URLLC
		switch group.Expectations.Latency {
		case "ultra-low":
			fiveQI = 85
		case "low":
			fiveQI = 82
		case "medium":
			fiveQI = 84
		default:
			fiveQI = 82 // Default for URLLC
		}
	case "eMBB":
		// Map bandwidth to 5QI for eMBB
		switch group.Expectations.Bandwidth {
		case "dedicated-high":
			fiveQI = 4
		case "high":
			fiveQI = 6
		case "standard":
			fiveQI = 9
		default:
			fiveQI = 9 // Default for eMBB
		}
	default:
		fiveQI = 9 // Default fallback
	}

	return &e2ev1alpha1.CoreDomainParams{
		FiveQI: fiveQI,
		QFI:    fiveQI, // QFI typically maps to 5QI value
	}
}

// translateToRANParams translates intent expectations to RAN domain parameters.
// Uses simple switch/case mapping as specified.
func translateToRANParams(group *e2ev1alpha1.IntentGroup) *e2ev1alpha1.RANDomainParams {
	var sst uint32 = 1 // Default SST for eMBB/URLLC
	var sd uint32
	var priority uint32
	var minPrb, maxPrb uint32

	// Map sliceType to SST/SD and priority
	switch group.Expectations.SliceType {
	case "URLLC":
		sd = 1122867   // 0x112233 - URLLC slice
		priority = 200 // High priority for URLLC
	case "eMBB":
		sd = 66051    // 0x010203 - eMBB slice
		priority = 10 // Lower priority for eMBB
	default:
		sd = 66051
		priority = 10
	}

	// Map resourceShare to PRB ratios
	switch group.Expectations.ResourceShare {
	case "Full":
		minPrb = 0
		maxPrb = 100
	case "Partial":
		minPrb = 0
		maxPrb = 50
	default:
		minPrb = 0
		maxPrb = 50
	}

	return &e2ev1alpha1.RANDomainParams{
		SST:               sst,
		SD:                sd,
		MinPrbPolicyRatio: minPrb,
		MaxPrbPolicyRatio: maxPrb,
		Priority:          priority,
	}
}

// SetupWithManager registers the controller with the Manager.
func (r *E2EQoSIntentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&e2ev1alpha1.E2EQoSIntent{}).
		Complete(r)
}
