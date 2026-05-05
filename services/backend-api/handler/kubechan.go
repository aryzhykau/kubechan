// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"net/http"

	k8s "github.com/org/kubechan/services/backend-api/k8s"
)

// KubeChan holds dependencies for KubeChan persona endpoints.
type KubeChan struct {
	MoodSyncer *k8s.MoodSyncer
}

// GetState handles GET /api/v1/kubechan/state
// Returns the current KubeChanState mood fields.
func (h *KubeChan) GetState(w http.ResponseWriter, r *http.Request) {
	level := h.MoodSyncer.GetMoodLevel(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"moodLevel": level,
	})
}

// Poke handles POST /api/v1/kubechan/poke
// Increments the poke streak counter and recomputes mood.
func (h *KubeChan) Poke(w http.ResponseWriter, r *http.Request) {
	state, err := h.MoodSyncer.Poke(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"moodLevel": int(state.Status.MoodLevel),
		"pokeCount": state.Status.PokeCount,
	})
}
