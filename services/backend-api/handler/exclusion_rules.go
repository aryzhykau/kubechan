// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// ExclusionRules holds dependencies for the exclusion rule management handlers.
//
// +kubebuilder:rbac:groups=kubechan.io,resources=kubechanexclusionrules,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=kubechan.io,resources=kubechanexclusionrules/status,verbs=get;patch
type ExclusionRules struct {
	K8s              client.Client
	DefaultNamespace string
}

// exclusionRuleResponse is the JSON shape returned by list/create.
type exclusionRuleResponse struct {
	Name   string                        `json:"name"`
	Spec   v1alpha1.ExclusionRuleSpec   `json:"spec"`
	Status v1alpha1.ExclusionRuleStatus `json:"status"`
}

// List handles GET /api/v1/exclusion-rules
func (h *ExclusionRules) List(w http.ResponseWriter, r *http.Request) {
	var list v1alpha1.KubechanExclusionRuleList
	if err := h.K8s.List(r.Context(), &list, client.InNamespace(h.DefaultNamespace)); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("listing exclusion rules: %s", err))
		return
	}
	resp := make([]exclusionRuleResponse, len(list.Items))
	for i, rule := range list.Items {
		resp[i] = exclusionRuleResponse{Name: rule.Name, Spec: rule.Spec, Status: rule.Status}
	}
	writeJSON(w, http.StatusOK, resp)
}

// createExclusionRuleRequest is the body for POST /api/v1/exclusion-rules.
type createExclusionRuleRequest struct {
	Name string                      `json:"name"`
	Spec v1alpha1.ExclusionRuleSpec `json:"spec"`
}

// Create handles POST /api/v1/exclusion-rules
func (h *ExclusionRules) Create(w http.ResponseWriter, r *http.Request) {
	var req createExclusionRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Spec.Description) == "" {
		writeError(w, http.StatusBadRequest, "spec.description is required")
		return
	}
	if len(req.Spec.TargetResources) == 0 && req.Spec.Selector == nil {
		writeError(w, http.StatusBadRequest, "at least one of spec.targetResources or spec.selector must be set")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Default enabled to true when not explicitly set to false.
	spec := req.Spec
	spec.Enabled = true

	rule := &v1alpha1.KubechanExclusionRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: h.DefaultNamespace,
		},
		Spec: spec,
	}
	if err := h.K8s.Create(r.Context(), rule); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("creating exclusion rule: %s", err))
		return
	}
	writeJSON(w, http.StatusCreated, exclusionRuleResponse{Name: rule.Name, Spec: rule.Spec, Status: rule.Status})
}

// setEnabledRequest is the body for PATCH /api/v1/exclusion-rules/{name}.
type setEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// SetEnabled handles PATCH /api/v1/exclusion-rules/{name}
func (h *ExclusionRules) SetEnabled(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var req setEnabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	rule := &v1alpha1.KubechanExclusionRule{}
	if err := h.K8s.Get(r.Context(), client.ObjectKey{Namespace: h.DefaultNamespace, Name: name}, rule); err != nil {
		writeError(w, http.StatusNotFound, "exclusion rule not found")
		return
	}

	patch := client.MergeFrom(rule.DeepCopy())
	rule.Spec.Enabled = req.Enabled
	if err := h.K8s.Patch(r.Context(), rule, patch); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("patching exclusion rule: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, exclusionRuleResponse{Name: rule.Name, Spec: rule.Spec, Status: rule.Status})
}

// Delete handles DELETE /api/v1/exclusion-rules/{name}
func (h *ExclusionRules) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	rule := &v1alpha1.KubechanExclusionRule{}
	if err := h.K8s.Get(r.Context(), client.ObjectKey{Namespace: h.DefaultNamespace, Name: name}, rule); err != nil {
		writeError(w, http.StatusNotFound, "exclusion rule not found")
		return
	}
	if err := h.K8s.Delete(r.Context(), rule); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("deleting exclusion rule: %s", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
