import type { EvidencePayload, K8sEvent } from '../../api/index'

export type FieldEntry =
  | { type: 'text';   key: string; value: string }
  | { type: 'logs';   key: string; value: string; truncated?: boolean }
  | { type: 'events'; key: string; value: K8sEvent[] }
  | { type: 'kv';     key: string; value: Record<string, string> }
  | { type: 'chips';  key: string; value: string[] }
  | { type: 'json';   key: string; value: unknown }

export interface ResourceCard {
  id: string
  kind: string
  name: string
  namespace?: string
  fields: FieldEntry[]
}

function isPrimitive(v: unknown): v is string | number | boolean {
  return typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean'
}

function isFlatStringRecord(v: unknown): v is Record<string, string> {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) return false
  return Object.values(v as Record<string, unknown>).every(val => typeof val === 'string')
}

export function buildResourceCards(payload: EvidencePayload): ResourceCard[] {
  const cards: ResourceCard[] = []

  // 1. Root resource
  if (payload.rootResource) {
    const { kind, name, namespace } = payload.rootResource
    const fields: FieldEntry[] = []
    if (payload.rootResourceEvents?.length) {
      fields.push({ type: 'events', key: 'Events', value: payload.rootResourceEvents })
    }
    cards.push({ id: `${kind}/${namespace ?? ''}/${name}`, kind, name, namespace, fields })
  }

  // 2. Workload pods
  for (const pod of payload.workloadPodLogs ?? []) {
    const fields: FieldEntry[] = []
    if (pod.phase) {
      fields.push({ type: 'text', key: 'Phase', value: pod.phase })
    }
    if (pod.logs) {
      fields.push({ type: 'logs', key: 'Logs', value: pod.logs })
    }
    if (pod.prevLogs) {
      fields.push({ type: 'logs', key: 'Previous container logs', value: pod.prevLogs })
    }
    if (pod.events?.length) {
      fields.push({ type: 'events', key: 'Events', value: pod.events })
    }
    for (const cm of pod.dependencies?.configMaps ?? []) {
      if (cm.data && Object.keys(cm.data).length > 0) {
        const label = cm.missing ? `ConfigMap: ${cm.name} (missing)` : `ConfigMap: ${cm.name}`
        fields.push({ type: 'kv', key: label, value: cm.data })
      }
    }
    const secretNames = (pod.dependencies?.secrets ?? []).map(s => s.name)
    if (secretNames.length) {
      fields.push({ type: 'chips', key: 'Secrets', value: secretNames })
    }
    cards.push({ id: `Pod//${pod.podName}`, kind: 'Pod', name: pod.podName, fields })
  }

  // 3. PVCs
  for (const pvc of payload.pvcInfos ?? []) {
    const fields: FieldEntry[] = []
    if (pvc.phase) {
      fields.push({ type: 'text', key: 'Phase', value: pvc.phase })
    }
    if (pvc.storageClass) {
      fields.push({ type: 'text', key: 'Storage Class', value: pvc.storageClass })
    }
    if (pvc.requestedStorage) {
      fields.push({ type: 'text', key: 'Requested Storage', value: pvc.requestedStorage })
    }
    if (pvc.events?.length) {
      fields.push({ type: 'events', key: 'Events', value: pvc.events })
    }
    cards.push({ id: `PersistentVolumeClaim//${pvc.name}`, kind: 'PersistentVolumeClaim', name: pvc.name, fields })
  }

  // 4. Related resources — includes CRDs fetched via the dynamic client (e.g. ScaledObject)
  // For those, rre.spec = { spec: {...}, status: {...} } — both values are nested objects → 'json'
  for (const rre of payload.relatedResourceEvidence ?? []) {
    const { kind, name, namespace } = rre.resource
    const fields: FieldEntry[] = []
    if (rre.events?.length) {
      fields.push({ type: 'events', key: 'Events', value: rre.events })
    }
    if (rre.logs) {
      fields.push({ type: 'logs', key: 'Logs', value: rre.logs })
    }
    for (const [k, v] of Object.entries(rre.spec ?? {})) {
      if (isPrimitive(v)) {
        fields.push({ type: 'text', key: k, value: String(v) })
      } else if (isFlatStringRecord(v)) {
        fields.push({ type: 'kv', key: k, value: v })
      } else {
        fields.push({ type: 'json', key: k, value: v })
      }
    }
    cards.push({ id: `${kind}/${namespace ?? ''}/${name}`, kind, name, namespace, fields })
  }

  return cards
}
