// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resources holds dependencies for K8s resource listing handlers.
type Resources struct {
	K8s client.Client
}

// controlPlaneNamespaces are always excluded from namespace listings.
var controlPlaneNamespaces = map[string]bool{
	"kube-system":       true,
	"kube-public":       true,
	"kube-node-lease":   true,
	"local-path-storage": true,
}

// ListNamespaces handles GET /api/v1/namespaces
// Returns all non-control-plane namespace names.
func (h *Resources) ListNamespaces(w http.ResponseWriter, r *http.Request) {
	var nsList corev1.NamespaceList
	if err := h.K8s.List(r.Context(), &nsList); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		if !controlPlaneNamespaces[ns.Name] {
			names = append(names, ns.Name)
		}
	}
	writeJSON(w, http.StatusOK, names)
}

// resourceItem is the response element for ListResources.
type resourceItem struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// ListResources handles GET /api/v1/namespaces/{ns}/resources?kind=Deployment
// Supported kinds: Deployment, StatefulSet, DaemonSet, Job, CronJob,
//
//	Pod, Service, PersistentVolumeClaim, ConfigMap
func (h *Resources) ListResources(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind == "" {
		writeError(w, http.StatusBadRequest, "kind query parameter is required")
		return
	}

	items, err := h.listByKind(r, ns, kind)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.HasPrefix(err.Error(), "unsupported kind") {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Resources) listByKind(r *http.Request, ns, kind string) ([]resourceItem, error) {
	ctx := r.Context()
	opts := []client.ListOption{client.InNamespace(ns)}

	switch kind {
	case "Deployment":
		var list appsv1.DeploymentList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "StatefulSet":
		var list appsv1.StatefulSetList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "DaemonSet":
		var list appsv1.DaemonSetList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "Job":
		var list batchv1.JobList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "CronJob":
		var list batchv1.CronJobList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "Pod":
		var list corev1.PodList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "Service":
		var list corev1.ServiceList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "PersistentVolumeClaim":
		var list corev1.PersistentVolumeClaimList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "ConfigMap":
		var list corev1.ConfigMapList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	case "Ingress":
		var list networkingv1.IngressList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, func() []string {
			names := make([]string, len(list.Items))
			for i, o := range list.Items {
				names[i] = o.Name
			}
			return names
		}()), nil

	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

func toItems(ns string, names []string) []resourceItem {
	out := make([]resourceItem, len(names))
	for i, n := range names {
		out[i] = resourceItem{Name: n, Namespace: ns}
	}
	return out
}
