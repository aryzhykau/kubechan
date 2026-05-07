// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/diagnostics-worker/collector"
)

// DiagnosticRunReconciler watches DiagnosticRun objects and runs evidence collection.
//
// +kubebuilder:rbac:groups=kubechan.io,resources=diagnosticruns,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=diagnosticruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=incidents,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments;replicasets;statefulsets;daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs;cronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch
type DiagnosticRunReconciler struct {
	client.Client
	Logger         *slog.Logger
	BackendAPIURL  string
	LogTailLines   int64
	PrevLogLines   int64
	CollectorVersion string
}

func (r *DiagnosticRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.With("diagnosticrun", req.NamespacedName)

	dr := &v1alpha1.DiagnosticRun{}
	if err := r.Get(ctx, req.NamespacedName, dr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Only handle pending runs.
	if dr.Status.State != v1alpha1.DiagnosticRunStatePending {
		return ctrl.Result{}, nil
	}

	log.Info("starting evidence collection")

	// Patch state → running.
	patch := client.MergeFrom(dr.DeepCopy())
	dr.Status.State = v1alpha1.DiagnosticRunStateRunning
	if err := r.Status().Patch(ctx, dr, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching state to running: %w", err)
	}

	// Determine which namespace + incident/problemcases to collect for.
	ns := dr.Namespace
	incidentName := dr.Spec.IncidentRef
	if incidentName == "" {
		incidentName = dr.Spec.ProblemCaseRef
	}

	evidence, collectionErrors := r.collect(ctx, ns, incidentName, dr.Name)

	// Patch state → completed (or failed if we have nothing at all).
	now := metav1.Now()
	patch2 := client.MergeFrom(dr.DeepCopy())
	if len(collectionErrors) > 0 && evidence == nil {
		dr.Status.State = v1alpha1.DiagnosticRunStateFailed
	} else {
		dr.Status.State = v1alpha1.DiagnosticRunStateCompleted
	}
	dr.Status.CollectedAt = &now
	dr.Status.CollectorVersion = r.CollectorVersion
	dr.Status.CollectionErrors = collectionErrors
	if err := r.Status().Patch(ctx, dr, patch2); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching state to completed: %w", err)
	}

	if evidence != nil {
		if err := r.postEvidence(ctx, dr, evidence, collectionErrors); err != nil {
			log.Error("failed to post evidence to backend-api", "err", err)
			// Don't requeue — DR is already completed. Backend will miss the evidence row,
			// but the DR status is already patched.
		}
	}

	log.Info("evidence collection done", "state", dr.Status.State, "errors", len(collectionErrors))
	return ctrl.Result{}, nil
}

// collect gathers all evidence for the incident/problemcase and returns the payload.
func (r *DiagnosticRunReconciler) collect(
	ctx context.Context,
	ns, incidentName, drName string,
) (*collector.Evidence, []string) {
	var errs []string
	ev := &collector.Evidence{
		DiagnosticRunID: drName,
		CollectedAt:     time.Now().UTC(),
	}

	// Resolve incident → list of ProblemCases.
	inc := &v1alpha1.Incident{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: incidentName}, inc); err != nil {
		errs = append(errs, fmt.Sprintf("get incident: %s", err))
		return ev, errs
	}
	ev.IncidentID = inc.Name
	ev.RootResource = collector.ResourceRef{
		Kind: inc.Spec.RootResource.Kind,
		Name: inc.Spec.RootResource.Name,
	}

	for _, pcName := range inc.Spec.ProblemCases {
		pc := &v1alpha1.ProblemCase{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: pcName}, pc); err != nil {
			errs = append(errs, fmt.Sprintf("get problemcase %s: %s", pcName, err))
			continue
		}

		pce := collector.ProblemCaseEvidence{
			Name:             pc.Name,
			Detector:         pc.Spec.Detector,
			Severity:         string(pc.Spec.Severity),
			Symptoms:         pc.Spec.Symptoms,
			AffectedResource: collector.ResourceRef{Kind: pc.Spec.AffectedResource.Kind, Name: pc.Spec.AffectedResource.Name},
		}

		// Use the actual namespace of the affected resource (may differ from control namespace).
		resourceNS := pc.Spec.AffectedResource.Namespace
		if resourceNS == "" {
			resourceNS = ns
		}

		// Collect events for the affected resource.
		events, err := r.collectEvents(ctx, resourceNS, pc.Spec.AffectedResource.Kind, pc.Spec.AffectedResource.Name)
		if err != nil {
			errs = append(errs, fmt.Sprintf("events for %s/%s: %s", pc.Spec.AffectedResource.Kind, pc.Spec.AffectedResource.Name, err))
		}
		pce.Events = events

		// If affected resource is a Pod, collect logs.
		if pc.Spec.AffectedResource.Kind == "Pod" {
			logs, truncated, err := r.collectPodLogs(ctx, resourceNS, pc.Spec.AffectedResource.Name)
			if err != nil {
				errs = append(errs, fmt.Sprintf("logs for pod %s: %s", pc.Spec.AffectedResource.Name, err))
			}
			pce.Logs = logs
			pce.LogsTruncated = truncated
		}

		ev.ProblemCases = append(ev.ProblemCases, pce)
	}

	// Use the actual namespace of the root resource for events and pod log collection.
	rootNS := inc.Spec.RootResource.Namespace
	if rootNS == "" {
		rootNS = ns
	}

	// Also collect events for the root resource.
	rootEvents, err := r.collectEvents(ctx, rootNS, inc.Spec.RootResource.Kind, inc.Spec.RootResource.Name)
	if err != nil {
		errs = append(errs, fmt.Sprintf("events for root %s/%s: %s", inc.Spec.RootResource.Kind, inc.Spec.RootResource.Name, err))
	}
	ev.RootResourceEvents = rootEvents

	// Collect pods owned by the root resource (in its actual namespace).
	pods, pvcs, err := r.collectWorkloadPodLogs(ctx, rootNS, inc.Spec.RootResource.Name)
	if err != nil {
		errs = append(errs, fmt.Sprintf("pod logs for workload %s: %s", inc.Spec.RootResource.Name, err))
	}
	ev.WorkloadPodLogs = pods
	ev.PVCInfos = pvcs

	// Populate user-provided context for manual incidents.
	ev.UserMessage = inc.Spec.UserMessage

	// Build a deduplicated set of related resources to collect:
	// start with user-tagged ones, then auto-discover Ingresses in the root namespace.
	type rrKey struct{ kind, ns, name string }
	seen := map[rrKey]bool{}

	type rrEntry struct{ kind, ns, name, apiGroup string }
	var toCollect []rrEntry

	for _, rr := range inc.Spec.RelatedResources {
		rrNS := rr.Namespace
		if rrNS == "" {
			rrNS = ns
		}
		k := rrKey{rr.Kind, rrNS, rr.Name}
		if !seen[k] {
			seen[k] = true
			toCollect = append(toCollect, rrEntry{rr.Kind, rrNS, rr.Name, rr.APIGroup})
		}
	}

	// Auto-discover all Ingresses in the root resource namespace so the LLM
	// can spot backend service name mismatches even when the user didn't tag them.
	if rootNS != "" {
		var ingressList networkingv1.IngressList
		if err := r.List(ctx, &ingressList, client.InNamespace(rootNS)); err == nil {
			for _, ing := range ingressList.Items {
				k := rrKey{"Ingress", rootNS, ing.Name}
				if !seen[k] {
					seen[k] = true
					toCollect = append(toCollect, rrEntry{"Ingress", rootNS, ing.Name, ""})
				}
			}
		} else {
			errs = append(errs, fmt.Sprintf("auto-discover ingresses in %s: %s", rootNS, err))
		}
	}

	// Collect events + spec for each related resource.
	for _, rr := range toCollect {
		rre := collector.RelatedResourceEvidence{
			Resource: collector.ResourceRef{Kind: rr.kind, Name: rr.name, APIGroup: rr.apiGroup},
		}
		events, err := r.collectEvents(ctx, rr.ns, rr.kind, rr.name)
		if err != nil {
			errs = append(errs, fmt.Sprintf("events for related %s/%s: %s", rr.kind, rr.name, err))
		}
		rre.Events = events
		spec, err := r.collectResourceSpec(ctx, rr.ns, rr.kind, rr.name, rr.apiGroup)
		if err != nil {
			errs = append(errs, fmt.Sprintf("spec for related %s/%s: %s", rr.kind, rr.name, err))
		}
		rre.Spec = spec
		ev.RelatedResourceEvidence = append(ev.RelatedResourceEvidence, rre)
	}

	return ev, errs
}

// collectResourceSpec fetches a resource and returns kind-specific diagnostic fields.
// The returned map is included verbatim in the evidence payload sent to the LLM.
func (r *DiagnosticRunReconciler) collectResourceSpec(ctx context.Context, ns, kind, name, apiGroup string) (map[string]any, error) {
	key := client.ObjectKey{Namespace: ns, Name: name}
	switch kind {
	case "Ingress":
		obj := &networkingv1.Ingress{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		var rules []map[string]any
		for _, rule := range obj.Spec.Rules {
			var paths []map[string]any
			if rule.HTTP != nil {
				for _, p := range rule.HTTP.Paths {
					path := map[string]any{
						"path":        p.Path,
						"pathType":    string(*p.PathType),
						"backendService": p.Backend.Service.Name,
						"backendPort": p.Backend.Service.Port.Number,
					}
					paths = append(paths, path)
				}
			}
			rules = append(rules, map[string]any{"host": rule.Host, "paths": paths})
		}
		spec := map[string]any{
			"ingressClassName": obj.Spec.IngressClassName,
			"rules":            rules,
			"annotations":      obj.Annotations,
		}
		if obj.Spec.IngressClassName == nil {
			spec["ingressClassName"] = nil
		}
		return spec, nil

	case "Service":
		obj := &corev1.Service{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		var ports []map[string]any
		for _, p := range obj.Spec.Ports {
			ports = append(ports, map[string]any{
				"name":       p.Name,
				"port":       p.Port,
				"targetPort": p.TargetPort.String(),
				"protocol":   string(p.Protocol),
			})
		}
		return map[string]any{
			"type":        string(obj.Spec.Type),
			"selector":    obj.Spec.Selector,
			"ports":       ports,
			"clusterIP":   obj.Spec.ClusterIP,
			"annotations": obj.Annotations,
		}, nil

	case "ConfigMap":
		obj := &corev1.ConfigMap{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		// Truncate individual values to 512 bytes so the LLM context stays manageable.
		data := make(map[string]string, len(obj.Data))
		for k, v := range obj.Data {
			if len(v) > 512 {
				data[k] = v[:512] + "…[truncated]"
			} else {
				data[k] = v
			}
		}
		return map[string]any{"data": data, "annotations": obj.Annotations}, nil

	case "Deployment":
		obj := &appsv1.Deployment{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		return map[string]any{
			"replicas":          obj.Spec.Replicas,
			"selector":          obj.Spec.Selector.MatchLabels,
			"availableReplicas": obj.Status.AvailableReplicas,
			"readyReplicas":     obj.Status.ReadyReplicas,
			"conditions":        summariseDeploymentConditions(obj.Status.Conditions),
		}, nil

	case "StatefulSet":
		obj := &appsv1.StatefulSet{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		return map[string]any{
			"replicas":        obj.Spec.Replicas,
			"readyReplicas":   obj.Status.ReadyReplicas,
			"currentReplicas": obj.Status.CurrentReplicas,
		}, nil

	case "DaemonSet":
		obj := &appsv1.DaemonSet{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		return map[string]any{
			"desiredNumberScheduled":  obj.Status.DesiredNumberScheduled,
			"numberReady":             obj.Status.NumberReady,
			"numberUnavailable":       obj.Status.NumberUnavailable,
		}, nil

	case "CronJob":
		obj := &batchv1.CronJob{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		var lastRun *string
		if obj.Status.LastScheduleTime != nil {
			s := obj.Status.LastScheduleTime.UTC().Format(time.RFC3339)
			lastRun = &s
		}
		var lastSuccessful *string
		if obj.Status.LastSuccessfulTime != nil {
			s := obj.Status.LastSuccessfulTime.UTC().Format(time.RFC3339)
			lastSuccessful = &s
		}
		return map[string]any{
			"schedule":           obj.Spec.Schedule,
			"suspend":            obj.Spec.Suspend,
			"activeJobs":         len(obj.Status.Active),
			"lastScheduleTime":   lastRun,
			"lastSuccessfulTime": lastSuccessful,
			"annotations":        obj.Annotations,
		}, nil

	case "Job":
		obj := &batchv1.Job{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		return map[string]any{
			"completions":  obj.Spec.Completions,
			"succeeded":    obj.Status.Succeeded,
			"failed":       obj.Status.Failed,
			"active":       obj.Status.Active,
			"startTime":    obj.Status.StartTime,
			"completionTime": obj.Status.CompletionTime,
		}, nil

	case "PersistentVolumeClaim":
		obj := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, key, obj); err != nil {
			return nil, err
		}
		capacity := ""
		if q, ok := obj.Status.Capacity[corev1.ResourceStorage]; ok {
			capacity = q.String()
		}
		return map[string]any{
			"phase":        string(obj.Status.Phase),
			"capacity":     capacity,
			"accessModes":  obj.Spec.AccessModes,
			"storageClass": obj.Spec.StorageClassName,
		}, nil

	default:
		// Unknown kind — try the dynamic client to return spec+status as-is.
		if apiGroup != "" {
			return r.collectDynamicSpec(ctx, ns, kind, name, apiGroup)
		}
		return nil, nil
	}
}

// collectDynamicSpec fetches an arbitrary CRD via the dynamic client and returns
// its .spec and .status as a flat map, giving the LLM visibility into non-core resources.
func (r *DiagnosticRunReconciler) collectDynamicSpec(ctx context.Context, ns, kind, name, apiGroup string) (map[string]any, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("get rest config: %w", err)
	}

	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}

	lists, _ := dc.ServerPreferredNamespacedResources()
	var gvr schema.GroupVersionResource
	found := false
	for _, list := range lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil || gv.Group != apiGroup {
			continue
		}
		for _, res := range list.APIResources {
			if strings.EqualFold(res.Kind, kind) {
				gvr = schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: res.Name}
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("kind %q apiGroup %q not found in cluster", kind, apiGroup)
	}

	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}

	obj, err := dynClient.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	out := map[string]any{}
	if spec, ok := obj.Object["spec"]; ok {
		out["spec"] = spec
	}
	if status, ok := obj.Object["status"]; ok {
		out["status"] = status
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func summariseDeploymentConditions(conds []appsv1.DeploymentCondition) []map[string]any {
	var out []map[string]any
	for _, c := range conds {
		out = append(out, map[string]any{
			"type":    string(c.Type),
			"status":  string(c.Status),
			"reason":  c.Reason,
			"message": c.Message,
		})
	}
	return out
}

func (r *DiagnosticRunReconciler) collectEvents(ctx context.Context, ns, kind, name string) ([]collector.K8sEvent, error) {
	list := &corev1.EventList{}
	if err := r.List(ctx, list, client.InNamespace(ns)); err != nil {
		return nil, err
	}
	var out []collector.K8sEvent
	for _, e := range list.Items {
		if e.InvolvedObject.Kind == kind && e.InvolvedObject.Name == name {
			out = append(out, collector.K8sEvent{
				Type:    e.Type,
				Reason:  e.Reason,
				Message: e.Message,
				Count:   e.Count,
				FirstTime: e.FirstTimestamp.Time,
				LastTime:  e.LastTimestamp.Time,
			})
		}
	}
	return out, nil
}

func (r *DiagnosticRunReconciler) collectPodLogs(ctx context.Context, ns, podName string) (string, bool, error) {
	// Use a direct REST client for log streaming — controller-runtime client doesn't support subresources like /log.
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return "", false, err
	}
	clientset, err := newCoreClientset(cfg)
	if err != nil {
		return "", false, err
	}
	tailLines := r.LogTailLines
	logs, err := clientset.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	}).DoRaw(ctx)
	if err != nil {
		// Try previous container logs as fallback.
		prev := r.PrevLogLines
		logs, err = clientset.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
			TailLines: &prev,
			Previous:  true,
		}).DoRaw(ctx)
		if err != nil {
			return "", false, err
		}
		return string(logs), false, nil
	}
	truncated := int64(len(bytes.Split(logs, []byte("\n")))) >= tailLines
	return string(logs), truncated, nil
}

func (r *DiagnosticRunReconciler) collectWorkloadPodLogs(ctx context.Context, ns, workloadName string) ([]collector.PodLogs, []collector.PVCInfo, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(ns)); err != nil {
		return nil, nil, err
	}

	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, nil, err
	}
	clientset, err := newCoreClientset(cfg)
	if err != nil {
		return nil, nil, err
	}

	seenPVCs := map[string]bool{}
	var podOut []collector.PodLogs
	var pvcOut []collector.PVCInfo

	for _, pod := range podList.Items {
		// Match pods that belong to the workload by label app or by name prefix.
		if !podBelongsToWorkload(pod, workloadName) {
			continue
		}

		pl := collector.PodLogs{PodName: pod.Name, Phase: string(pod.Status.Phase)}

		// Collect pod logs.
		tailLines := r.LogTailLines
		raw, err := clientset.CoreV1().Pods(ns).GetLogs(pod.Name, &corev1.PodLogOptions{
			TailLines: &tailLines,
		}).DoRaw(ctx)
		if err != nil {
			pl.Error = err.Error()
		} else {
			pl.Logs = string(raw)
			pl.Truncated = int64(len(bytes.Split(raw, []byte("\n")))) >= tailLines
		}

		// Collect pod-level events (e.g. FailedScheduling, BackOff, Pulling).
		podEvents, _ := r.collectEvents(ctx, ns, "Pod", pod.Name)
		pl.Events = podEvents

		// Collect ConfigMap and Secret dependencies (existence only — no content).
		pl.Dependencies = r.collectPodDependencies(ctx, ns, pod)

		podOut = append(podOut, pl)

		// Collect PVC info for each volume the pod references.
		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim == nil {
				continue
			}
			claimName := vol.PersistentVolumeClaim.ClaimName
			if seenPVCs[claimName] {
				continue
			}
			seenPVCs[claimName] = true

			pvc := &corev1.PersistentVolumeClaim{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: claimName}, pvc); err != nil {
				continue
			}
			pi := collector.PVCInfo{
				Name:  claimName,
				Phase: string(pvc.Status.Phase),
			}
			if pvc.Spec.StorageClassName != nil {
				pi.StorageClass = *pvc.Spec.StorageClassName
			}
			if q, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				pi.RequestedStorage = q.String()
			}
			pvcEvents, _ := r.collectEvents(ctx, ns, "PersistentVolumeClaim", claimName)
			pi.Events = pvcEvents
			pvcOut = append(pvcOut, pi)
		}
	}
	return podOut, pvcOut, nil
}

// collectPodDependencies inspects a pod spec for ConfigMap and Secret references,
// fetches ConfigMap contents (keys + values), and records where each CM is volume-mounted.
// Secret values are never read.
func (r *DiagnosticRunReconciler) collectPodDependencies(ctx context.Context, ns string, pod corev1.Pod) *collector.PodDependencies {
	cmNames := map[string]bool{}
	secretNames := map[string]bool{}

	// vol name → ConfigMap name (for resolving mount paths below)
	volCMName := map[string]string{}
	// ConfigMap name → mount paths across all containers
	cmMountPaths := map[string][]string{}

	// Walk volumes to build volCMName mapping.
	for _, vol := range pod.Spec.Volumes {
		if vol.ConfigMap != nil {
			cmNames[vol.ConfigMap.Name] = true
			volCMName[vol.Name] = vol.ConfigMap.Name
		}
		if vol.Secret != nil {
			secretNames[vol.Secret.SecretName] = true
		}
		if vol.Projected != nil {
			for _, src := range vol.Projected.Sources {
				if src.ConfigMap != nil {
					cmNames[src.ConfigMap.Name] = true
					// projected volumes share the volume name
					volCMName[vol.Name] = src.ConfigMap.Name
				}
				if src.Secret != nil {
					secretNames[src.Secret.Name] = true
				}
			}
		}
	}

	// Walk all containers (including init containers).
	allContainers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, c := range allContainers {
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef != nil {
				cmNames[ef.ConfigMapRef.Name] = true
			}
			if ef.SecretRef != nil {
				secretNames[ef.SecretRef.Name] = true
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil {
				cmNames[e.ValueFrom.ConfigMapKeyRef.Name] = true
			}
			if e.ValueFrom.SecretKeyRef != nil {
				secretNames[e.ValueFrom.SecretKeyRef.Name] = true
			}
		}
		// Collect volume mount paths per ConfigMap.
		for _, vm := range c.VolumeMounts {
			if cmName, ok := volCMName[vm.Name]; ok {
				cmMountPaths[cmName] = appendUnique(cmMountPaths[cmName], vm.MountPath)
			}
		}
	}

	if len(cmNames) == 0 && len(secretNames) == 0 {
		return nil
	}

	const maxValueBytes = 1024

	deps := &collector.PodDependencies{}
	for name := range cmNames {
		cm := &corev1.ConfigMap{}
		err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, cm)
		ref := collector.ConfigMapRef{
			Name:       name,
			Missing:    apierrors.IsNotFound(err),
			MountPaths: cmMountPaths[name],
		}
		if err == nil && len(cm.Data) > 0 {
			ref.Data = make(map[string]string, len(cm.Data))
			for k, v := range cm.Data {
				ref.Keys = append(ref.Keys, k)
				if len(v) > maxValueBytes {
					ref.Data[k] = v[:maxValueBytes] + "...[truncated]"
				} else {
					ref.Data[k] = v
				}
			}
		}
		deps.ConfigMaps = append(deps.ConfigMaps, ref)
	}
	for name := range secretNames {
		sec := &corev1.Secret{}
		err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, sec)
		deps.Secrets = append(deps.Secrets, collector.SecretRef{
			Name:    name,
			Missing: apierrors.IsNotFound(err),
		})
	}
	return deps
}

// appendUnique appends s to slice only if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

func podBelongsToWorkload(pod corev1.Pod, workloadName string) bool {
	// Check owner references.
	for _, ref := range pod.OwnerReferences {
		if ref.Name == workloadName {
			return true
		}
	}
	// Check common labels.
	for _, label := range []string{"app", "app.kubernetes.io/name", "app.kubernetes.io/instance"} {
		if pod.Labels[label] == workloadName {
			return true
		}
	}
	return false
}

func (r *DiagnosticRunReconciler) postEvidence(ctx context.Context, dr *v1alpha1.DiagnosticRun, ev *collector.Evidence, errs []string) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}

	body := map[string]any{
		"diagnosticRunId":  dr.Name,
		"incidentId":       dr.Spec.IncidentRef,
		"collectedAt":      ev.CollectedAt.Format(time.RFC3339),
		"collectorVersion": r.CollectorVersion,
		"payload":          json.RawMessage(payload),
		"payloadBytes":     len(payload),
		"collectionErrors": errs,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}

	postCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	url := r.BackendAPIURL + "/internal/evidence"
	resp, err := http.Post(url, "application/json", bytes.NewReader(bodyBytes)) //nolint:noctx
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if postCtx.Err() != nil {
		return postCtx.Err()
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("backend returned %d", resp.StatusCode)
	}
	return nil
}

// SetupWithManager registers the reconciler and applies a predicate to only reconcile pending DRs.
func (r *DiagnosticRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DiagnosticRun{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			dr, ok := obj.(*v1alpha1.DiagnosticRun)
			if !ok {
				return true
			}
			return dr.Status.State == v1alpha1.DiagnosticRunStatePending || dr.Status.State == ""
		})).
		Complete(r)
}
