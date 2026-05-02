import { useState, useCallback } from 'react'
import { IncidentList } from './IncidentList'
import { KubeChanSidebar, type KubeChanState } from './KubeChanSidebar'
import type { AnalysisResult } from './api'
import './app.css'

function App() {
  const [kubechan, setKubechan] = useState<KubeChanState>({ pose: 'idle' })

  const handleAnalysisStart = useCallback((incidentName: string) => {
    setKubechan({ pose: 'thinking', incidentName })
  }, [])

  const handleAnalysisComplete = useCallback((result: AnalysisResult, incidentName: string) => {
    setKubechan({ pose: 'speaking', incidentName, result })
  }, [])

  return (
    <div className="app">
      <header className="app-header">
        <span className="app-logo">⎈</span>
        <h1>KubeChan</h1>
        <span className="app-subtitle">Kubernetes Incident Monitor</span>
      </header>
      <div className="app-body">
        <main className="app-main">
          <IncidentList
            onAnalysisStart={handleAnalysisStart}
            onAnalysisComplete={handleAnalysisComplete}
          />
        </main>
        <KubeChanSidebar state={kubechan} />
      </div>
    </div>
  )
}

export default App
