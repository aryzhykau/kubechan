# Incident fixture suite

This directory contains ten intentionally broken, isolated Kubernetes scenarios for exercising KubeChan from a single fault to a multi-point outage. Every scenario owns a namespace named `kubechan-incident-NN`; deleting that namespace removes the complete fixture.

## Run

The active context must be the same kind cluster that runs KubeChan.

```bash
# all scenarios
kubectl apply -k hack/incidents

# one scenario
kubectl apply -f hack/incidents/04-ingress-renamed-service.yaml

# observe automatic detection
kubectl get problemcases,incidents -A -w

# remove all scenarios and their generated namespaced KubeChan CRs
kubectl delete -k hack/incidents
```

The development chart uses a 5-second debounce and 60-second Pending/DeploymentUnavailable thresholds. Image pulls, container restarts, EndpointSlices, and controller reconciliation are asynchronous, so the complete set does not appear immediately. After an Incident appears, request a fresh analysis in the UI; existing DiagnosticRuns are immutable snapshots.

## Scenario matrix

| Level | Namespace | Fault | Expected signal/relationship |
|---:|---|---|---|
| 1 | `kubechan-incident-01` | nonexistent image | `ImagePullBackOff` |
| 2 | `kubechan-incident-02` | required setting absent at startup | `CrashLoopBackOff`; cause in previous logs |
| 3 | `kubechan-incident-03` | Service selects `version=v1`, Pod is `v2` | `ServiceNoEndpoints`; Service has no matching Pod |
| 4 | `kubechan-incident-04` | workload crashes while Ingress uses the former Service name | `CrashLoopBackOff`, no endpoints, broken `Ingress → missing Service`, and a strong renamed-service candidate |
| 5 | `kubechan-incident-05` | ConfigMap key and read path differ | `CrashLoopBackOff`; `Pod → ConfigMap` plus log evidence |
| 6 | `kubechan-incident-06` | referenced Secret does not exist | Pending/container-config events; missing `Pod → Secret`; no Service endpoints |
| 7 | `kubechan-incident-07` | PVC requests a nonexistent StorageClass | `PendingTooLong`; `Pod → PVC` and storage events |
| 8 | `kubechan-incident-08` | process never opens the probed port | probe events, restarts, and `ServiceNoEndpoints` |
| 9 | `kubechan-incident-09` | database crashes and dependent API remains unready | several correlated symptoms across database, API, Services, and Ingress |
| 10 | `kubechan-incident-10` | stale Ingress, wrong selector, missing ConfigMap/Secret, and unbound PVC | multiple simultaneous failures and observed/proposed topology |

## Analysis notes

Automatic incidents contain the resources attached by their detectors, and the diagnostics worker always auto-discovers Ingresses in the root namespace. To inspect all graph relationships in levels 3, 4, 9, and 10, report a manual incident for the named Deployment and add the scenario's Service(s), ConfigMap, Secret, or PVC as related resources. This exercises the same evidence and analysis pipeline while making those resources explicit in the immutable snapshot.

These fixtures only create broken test workloads. They do not create or modify KubeChan CRDs directly; detection, correlation, evidence collection, and analysis remain under test.
