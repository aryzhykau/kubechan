// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resources holds dependencies for K8s resource listing handlers.
type Resources struct {
	K8s    client.Client
	Config *rest.Config
}

// controlPlaneNamespaces are always excluded from namespace listings.
var controlPlaneNamespaces = map[string]bool{
	"kube-system":        true,
	"kube-public":        true,
	"kube-node-lease":    true,
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

// kindItem is the response element for ListKinds.
type kindItem struct {
	Kind     string `json:"kind"`
	APIGroup string `json:"apiGroup"`
}

// ListKinds handles GET /api/v1/namespaces/{ns}/kinds?q=Scaled
// Returns all namespaced resource kinds known to the API server that support get+list,
// sorted alphabetically. Tolerates partial discovery failures (e.g. unavailable aggregated APIs).
func (h *Resources) ListKinds(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	dc, err := discovery.NewDiscoveryClientForConfig(h.Config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("creating discovery client: %s", err))
		return
	}

	// ServerPreferredNamespacedResources tolerates partial failures — it returns what it can.
	lists, _ := dc.ServerPreferredNamespacedResources()

	seen := map[string]bool{}
	var items []kindItem

	for _, list := range lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, res := range list.APIResources {
			if res.Namespaced && hasVerbs(res.Verbs, "get", "list") {
				kind := res.Kind
				if q != "" && !strings.HasPrefix(strings.ToLower(kind), q) {
					continue
				}
				key := kind + "/" + gv.Group
				if seen[key] {
					continue
				}
				seen[key] = true
				items = append(items, kindItem{Kind: kind, APIGroup: gv.Group})
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].APIGroup < items[j].APIGroup
	})

	writeJSON(w, http.StatusOK, items)
}

// resourceItem is the response element for ListResources.
type resourceItem struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// ListResources handles GET /api/v1/namespaces/{ns}/resources?kind=Deployment&apiGroup=apps
// For known core/apps kinds the existing typed path is used.
// For any other kind (or when apiGroup is explicitly provided) the dynamic client is used,
// allowing arbitrary CRDs to be listed.
func (h *Resources) ListResources(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	apiGroup := strings.TrimSpace(r.URL.Query().Get("apiGroup"))

	if kind == "" {
		writeError(w, http.StatusBadRequest, "kind query parameter is required")
		return
	}

	// Use the typed fast path for well-known core/apps kinds when no explicit apiGroup override.
	if apiGroup == "" {
		items, err := h.listByKindTyped(r, ns, kind)
		if err == nil {
			writeJSON(w, http.StatusOK, items)
			return
		}
		if !strings.HasPrefix(err.Error(), "unsupported kind") {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Fall through to dynamic path for unsupported kinds.
	}

	items, err := h.listByKindDynamic(r, ns, kind, apiGroup)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// listByKindTyped handles the hardcoded known kinds — same as the original implementation.
func (h *Resources) listByKindTyped(r *http.Request, ns, kind string) ([]resourceItem, error) {
	ctx := r.Context()
	opts := []client.ListOption{client.InNamespace(ns)}

	switch kind {
	case "Deployment":
		var list appsv1.DeploymentList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		return toItems(ns, deploymentNames(list.Items)), nil
	case "StatefulSet":
		var list appsv1.StatefulSetList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	case "DaemonSet":
		var list appsv1.DaemonSetList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	case "Job":
		var list batchv1.JobList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	case "CronJob":
		var list batchv1.CronJobList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	case "Pod":
		var list corev1.PodList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	case "Service":
		var list corev1.ServiceList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	case "PersistentVolumeClaim":
		var list corev1.PersistentVolumeClaimList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	case "ConfigMap":
		var list corev1.ConfigMapList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	case "Ingress":
		var list networkingv1.IngressList
		if err := h.K8s.List(ctx, &list, opts...); err != nil {
			return nil, err
		}
		names := make([]string, len(list.Items))
		for i, o := range list.Items {
			names[i] = o.Name
		}
		return toItems(ns, names), nil
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

// listByKindDynamic resolves kind+apiGroup to a GVR via discovery and lists with the dynamic client.
func (h *Resources) listByKindDynamic(r *http.Request, ns, kind, apiGroup string) ([]resourceItem, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(h.Config)
	if err != nil {
		return nil, fmt.Errorf("creating discovery client: %w", err)
	}

	lists, _ := dc.ServerPreferredNamespacedResources()
	var gvr schema.GroupVersionResource
	found := false

	for _, list := range lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		if apiGroup != "" && gv.Group != apiGroup {
			continue
		}
		for _, res := range list.APIResources {
			if strings.EqualFold(res.Kind, kind) && hasVerbs(res.Verbs, "get", "list") {
				gvr = schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: res.Name,
				}
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("kind %q (apiGroup %q) not found in cluster", kind, apiGroup)
	}

	dynClient, err := dynamic.NewForConfig(h.Config)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	unstructList, err := dynClient.Resource(gvr).Namespace(ns).List(r.Context(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing %s/%s: %w", ns, kind, err)
	}

	items := make([]resourceItem, len(unstructList.Items))
	for i, obj := range unstructList.Items {
		items[i] = resourceItem{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	}
	return items, nil
}

func toItems(ns string, names []string) []resourceItem {
	out := make([]resourceItem, len(names))
	for i, n := range names {
		out[i] = resourceItem{Name: n, Namespace: ns}
	}
	return out
}

func deploymentNames(items []appsv1.Deployment) []string {
	names := make([]string, len(items))
	for i, o := range items {
		names[i] = o.Name
	}
	return names
}

// hasVerbs checks that all required verbs are present in the resource's verb list.
func hasVerbs(verbs []string, required ...string) bool {
	set := make(map[string]bool, len(verbs))
	for _, v := range verbs {
		set[v] = true
	}
	for _, req := range required {
		if !set[req] {
			return false
		}
	}
	return true
}
