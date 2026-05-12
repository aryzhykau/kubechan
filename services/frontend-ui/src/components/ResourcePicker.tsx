import { useState, useCallback } from 'react'
import type React from 'react'
import {
  Box, FormControl, InputLabel, Select, MenuItem,
  Autocomplete, TextField, Stack, Chip, CircularProgress,
} from '@mui/material'
import { useListKindsQuery, useListResourcesQuery } from '../store/api/resourcesApi'
import type { KindItem } from '../api/index'

const DEFAULT_ON_SLICES  = ['spec', 'status', 'conditions', 'events', 'labels']
const DEFAULT_OFF_SLICES = ['annotations', 'ownerRefs']
const WORKLOAD_SLICES    = ['logs', 'metrics']
const WORKLOAD_KINDS     = new Set(['Deployment', 'StatefulSet', 'DaemonSet', 'Pod', 'Job'])

export interface ResourceEntry {
  namespace: string
  kind: string
  apiGroup: string
  name: string
  evidenceSlices: string[]
}

// eslint-disable-next-line @typescript-eslint/no-unused-vars
function defaultSlices(_kind: string): string[] {
  return [...DEFAULT_ON_SLICES]
}

interface Props {
  value: ResourceEntry | null
  onChange: (entry: ResourceEntry | null) => void
  namespaces: string[]
  defaultNamespace?: string
  label?: string
}

export function ResourcePicker({ value, onChange, namespaces, defaultNamespace = '', label = '' }: Props) {
  const [ns, setNs]       = useState(value?.namespace || defaultNamespace || (namespaces[0] ?? ''))
  const [kind, setKind]   = useState<KindItem | null>(value ? { kind: value.kind, apiGroup: value.apiGroup } : null)
  const [kindQ, setKindQ] = useState('')
  const [name, setName]   = useState(value?.name ?? '')
  const [slices, setSlices] = useState<string[]>(value?.evidenceSlices ?? defaultSlices(value?.kind ?? ''))

  const { data: rawKindData, isLoading: loadingKinds } = useListKindsQuery(
    { ns, q: kindQ || undefined },
    { skip: !ns }
  )
  const kindOptions: KindItem[] = rawKindData ?? []

  const { data: rawNameData, isLoading: loadingNames } = useListResourcesQuery(
    { ns, kind: kind?.kind ?? '', apiGroup: kind?.apiGroup },
    { skip: !ns || !kind }
  )
  const nameOptions: import('../api/index').ResourceItem[] = rawNameData ?? []

  const notify = useCallback((nextNs: string, nextKind: KindItem | null, nextName: string, nextSlices: string[]) => {
    if (nextKind && nextName) {
      onChange({ namespace: nextNs, kind: nextKind.kind, apiGroup: nextKind.apiGroup, name: nextName, evidenceSlices: nextSlices })
    } else {
      onChange(null)
    }
  }, [onChange])

  function handleNs(newNs: string) { setNs(newNs); setKind(null); setName(''); notify(newNs, null, '', slices) }
  function handleKind(newKind: KindItem | null) { setKind(newKind); setName(''); notify(ns, newKind, '', slices) }
  function handleName(newName: string) { setName(newName); notify(ns, kind, newName, slices) }

  function toggleSlice(slice: string) {
    const next = slices.includes(slice) ? slices.filter(s => s !== slice) : [...slices, slice]
    setSlices(next)
    notify(ns, kind, name, next)
  }

  const showWorkloadSlices = kind ? WORKLOAD_KINDS.has(kind.kind) : false
  const allSlices = showWorkloadSlices
    ? [...DEFAULT_ON_SLICES, ...DEFAULT_OFF_SLICES, ...WORKLOAD_SLICES]
    : [...DEFAULT_ON_SLICES, ...DEFAULT_OFF_SLICES]

  const pfx = label ? `${label}-` : ''

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', gap: 1 }}>
        <FormControl size="small" sx={{ minWidth: 130 }}>
          <InputLabel id={`${pfx}ns-label`} sx={{ fontSize: '0.82rem' }}>Namespace</InputLabel>
          <Select labelId={`${pfx}ns-label`} value={ns} label="Namespace" onChange={e => handleNs(e.target.value)} sx={{ fontSize: '0.82rem' }}>
            {namespaces.map(n => <MenuItem key={n} value={n} sx={{ fontSize: '0.82rem' }}>{n}</MenuItem>)}
          </Select>
        </FormControl>

        <Autocomplete
          size="small"
          options={kindOptions}
          getOptionLabel={o => o.apiGroup ? `${o.apiGroup}/${o.kind}` : o.kind}
          isOptionEqualToValue={(a, b) => a.kind === b.kind && a.apiGroup === b.apiGroup}
          value={kind}
          inputValue={kindQ}
          onInputChange={(_, v) => setKindQ(v)}
          onChange={(_, v) => handleKind(v)}
          loading={loadingKinds}
          sx={{ minWidth: 180 }}
          renderInput={params => (
            <TextField {...params} label="Kind"
              slotProps={{
                input: { ...params.slotProps.input, endAdornment: <>{loadingKinds && <CircularProgress size={14} />}{(params.slotProps.input as { endAdornment?: React.ReactNode })?.endAdornment}</> },
                htmlInput: params.slotProps.htmlInput,
              }}
              sx={{ '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }}
            />
          )}
          renderOption={(props, option) => (
            <li {...props} key={`${option.apiGroup}/${option.kind}`} style={{ fontSize: '0.82rem' }}>
              {option.apiGroup ? <span><strong>{option.kind}</strong> <span style={{ opacity: 0.55 }}>{option.apiGroup}</span></span> : option.kind}
            </li>
          )}
        />

        <Autocomplete
          size="small" freeSolo
          options={nameOptions.map(r => r.name)}
          value={name} inputValue={name}
          onInputChange={(_, v) => handleName(v)}
          onChange={(_, v) => handleName(v ?? '')}
          loading={loadingNames} disabled={!kind}
          sx={{ minWidth: 180, flex: 1 }}
          renderInput={params => (
            <TextField {...params} label="Name"
              slotProps={{
                input: { ...params.slotProps.input, endAdornment: <>{loadingNames && <CircularProgress size={14} />}{(params.slotProps.input as { endAdornment?: React.ReactNode })?.endAdornment}</> },
                htmlInput: params.slotProps.htmlInput,
              }}
              sx={{ '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }}
            />
          )}
        />
      </Stack>

      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.75, alignItems: 'center' }}>
        {allSlices.map(slice => (
          <Chip key={slice} label={slice} size="small"
            variant={slices.includes(slice) ? 'filled' : 'outlined'}
            onClick={() => toggleSlice(slice)}
            sx={{ fontFamily: 'monospace', fontSize: '0.7rem', height: 22, cursor: 'pointer',
              ...(slices.includes(slice)
                ? { bgcolor: 'rgba(99,102,241,0.25)', color: '#818cf8', borderColor: 'transparent' }
                : { color: '#4a4a62', borderColor: '#2a2a42' }) }} />
        ))}
      </Stack>
    </Box>
  )
}
