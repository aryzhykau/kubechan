export function fmtDate(iso: string, includeSeconds = false): string {
  if (!iso) return '—'
  try {
    const opts: Intl.DateTimeFormatOptions = {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }
    if (includeSeconds) opts.second = '2-digit'
    return new Date(iso).toLocaleString(undefined, opts)
  } catch {
    return iso
  }
}

export function timeAgo(iso?: string): string {
  if (!iso) return '—'
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}
