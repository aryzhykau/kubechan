import { useEffect, useState } from 'react'
import { useGetAnalysisProgressQuery } from '../store/api/diagnosticsApi'

export function useAnalysisProgress(runId: string, enabled = true) {
  const [pollingInterval, setPollingInterval] = useState(3000)
  const query = useGetAnalysisProgressQuery(runId, { skip: !enabled || !runId, pollingInterval, refetchOnMountOrArgChange: true })
  useEffect(() => {
    const terminal = ['completed', 'partial', 'failed', 'cancelled', 'timed_out', 'interrupted']
    setPollingInterval(terminal.includes(query.currentData?.status ?? '') ? 0 : 3000)
  }, [query.currentData?.status, runId])
  return { ...query, data: query.currentData }
}
