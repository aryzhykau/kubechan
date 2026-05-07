import { useState, useEffect, useCallback } from 'react'
import type React from 'react'
import {
  Box,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Autocomplete,
  TextField,
  Stack,
  Chip,
  CircularProgress,
} from '@mui/material'
import { api, type KindItem, type ResourceItem } from './api'

// Slices shown for all resource kinds (default ON)
const DEFAULT_ON_SLICES = ['spec', 'status', 'conditions', 'events', 'labels']
// Slices shown for all resource kinds (default OFF)
const DEFAULT_OFF_SLICES = ['annotations', 'ownerRefs']
// Slices only shown for workload kinds (default OFF)
const WORKLOAD_SLICES = ['logs', 'metrics']
const WORKLOAD_KINDS = new Set(['Deployment', 'StatefulSet', 'DaemonSet', 'Pod', 'Job'])

export interface ResourceEntry {
  namespace: string
  kind: string
  apiGroup: string
  name: string
  evidenceSlices: string[]
}

function defaultSlices(kind: string): string[] {
  const slices = [...DEFAULT_ON_SLICES]
  if (WORKLOAD_KINDS.has(kind)) {
    // workload kinds default OFF for logs/metrics — user toggles them on
  }
  return slices
}

interface Props {
  value: ResourceEntry | null
  onChange: (entry: ResourceEntry | null) => void
  namespaces: string[]
  defaultNamespace?: string
  /** Optional label prefix for accessibility */
  label?: string
}

export function ResourcePicker({ value, onChange, namespaces, defaultNamespace = '', label = '' }: Props) {
  const [ns, setNs] = useState(value?.namespace || defaultNamespace || (namespaces[0] ?? ''))
  const [kind, setKind] = useState<KindItem | null>(
    value ? { kind: value.kind, apiGroup: value.apiGroup } : null
  )
  const [kindQ, setKindQ] = useState('')
  const [kindOptions, setKindOptions] = useState<KindItem[]>([])
  const [loadingKinds, setLoadingKinds] = useState(false)
  const [name, setName] = useState(value?.name ?? '')
  const [nameOptions, setNameOptions] = useState<ResourceItem[]>([])
  const [loadingNames, setLoadingNames] = useState(false)
  const [slices, setSlices] = useState<string[]>(value?.evidenceSlices ?? defaultSlices(value?.kind ?? ''))

  // When namespace or kind query changes, refresh kind options
  useEffect(() => {
    if (!ns) { setKindOptions([]); return }
    setLoadingKinds(true)
    api.listKinds(ns, kindQ || undefined)
      .then(setKindOptions)
      .catch(() => setKindOptions([]))
      .finally(() => setLoadingKinds(false))
  }, [ns, kindQ])

  // When namespace or kind changes, refresh name options
  useEffect(() => {
    if (!ns || !kind) { setNameOptions([]); setName(''); return }
    setLoadingNames(true)
    setName('')
    api.listResources(ns, kind.kind, kind.apiGroup || undefined)
      .then(setNameOptions)
      .catch(() => setNameOptions([]))
      .finally(() => setLoadingNames(false))
  }, [ns, kind])

  // When kind changes, reset slices to defaults for the new kind
  useEffect(() => {
    setSlices(defaultSlices(kind?.kind ?? ''))
  }, [kind?.kind])

  const notify = useCallback((
    nextNs: string,
    nextKind: KindItem | null,
    nextName: string,
    nextSlices: string[],
  ) => {
    if (nextKind && nextName) {
      onChange({
        namespace: nextNs,
        kind: nextKind.kind,
        apiGroup: nextKind.apiGroup,
        name: nextName,
        evidenceSlices: nextSlices,
      })
    } else {
      onChange(null)
    }
  }, [onChange])

  function handleNs(newNs: string) {
    setNs(newNs)
    setKind(null)
    setName('')
    notify(newNs, null, '', slices)
  }

  function handleKind(newKind: KindItem | null) {
    setKind(newKind)
    setName('')
    notify(ns, newKind, '', slices)
  }

  function handleName(newName: string) {
    setName(newName)
    notify(ns, kind, newName, slices)
  }

  function toggleSlice(slice: string) {
    const next = slices.includes(slice)
      ? slices.filter(s => s !== slice)
      : [...slices, slice]
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
      {/* Row 1: namespace + kind + name */}
      <Stack direction="row" spacing={1} sx={{ flexWrap: 'wrap', gap: 1 }}>
        {/* Namespace */}
        <FormControl size="small" sx={{ minWidth: 130 }}>
          <InputLabel id={`${pfx}ns-label`} sx={{ fontSize: '0.82rem' }}>Namespace</InputLabel>
          <Select
            labelId={`${pfx}ns-label`}
            value={ns}
            label="Namespace"
            onChange={e => handleNs(e.target.value)}
            sx={{ fontSize: '0.82rem' }}
          >
            {namespaces.map(n => (
              <MenuItem key={n} value={n} sx={{ fontSize: '0.82rem' }}>{n}</MenuItem>
            ))}
          </Select>
        </FormControl>

        {/* Kind */}
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
            <TextField
              {...params}
              label="Kind"
              slotProps={{
                input: {
                  ...params.slotProps.input,
                  endAdornment: (
                    <>
                      {loadingKinds && <CircularProgress size={14} />}
                      {(params.slotProps.input as { endAdornment?: React.ReactNode })?.endAdornment}
                    </>
                  ),
                },
                htmlInput: params.slotProps.htmlInput,
              }}
              sx={{ '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }}
            />
          )}
          renderOption={(props, option) => (
            <li {...props} key={`${option.apiGroup}/${option.kind}`} style={{ fontSize: '0.82rem' }}>
              {option.apiGroup ? (
                <span><strong>{option.kind}</strong> <span style={{ opacity: 0.55 }}>{option.apiGroup}</span></span>
              ) : option.kind}
            </li>
          )}
        />

        {/* Name */}
        <Autocomplete
          size="small"
          freeSolo
          options={nameOptions.map(r => r.name)}
          value={name}
          inputValue={name}
          onInputChange={(_, v) => handleName(v)}
          onChange={(_, v) => handleName(v ?? '')}
          loading={loadingNames}
          disabled={!kind}
          sx={{ minWidth: 180, flex: 1 }}
          renderInput={params => (
            <TextField
              {...params}
              label="Name"
              slotProps={{
                input: {
                  ...params.slotProps.input,
                  endAdornment: (
                    <>
                      {loadingNames && <CircularProgress size={14} />}
                      {(params.slotProps.input as { endAdornment?: React.ReactNode })?.endAdornment}
                    </>
                  ),
                },
                htmlInput: params.slotProps.htmlInput,
              }}
              sx={{ '& .MuiInputBase-input': { fontSize: '0.82rem' }, '& .MuiInputLabel-root': { fontSize: '0.82rem' } }}
            />
          )}
        />
      </Stack>

      {/* Row 2: evidence slice chips */}
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.75, alignItems: 'center' }}>
        {allSlices.map(slice => (
          <Chip
            key={slice}
            label={slice}
            size="small"
            variant={slices.includes(slice) ? 'filled' : 'outlined'}
            onClick={() => toggleSlice(slice)}
            sx={{
              fontFamily: 'monospace',
              fontSize: '0.7rem',
              height: 22,
              cursor: 'pointer',
              ...(slices.includes(slice)
                ? { bgcolor: 'rgba(99,102,241,0.25)', color: '#818cf8', borderColor: 'transparent' }
                : { color: '#4a4a62', borderColor: '#2a2a42' }),
            }}
          />
        ))}
      </Stack>
    </Box>
  )
}
