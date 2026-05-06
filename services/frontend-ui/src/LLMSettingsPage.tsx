import { useState, useEffect, useCallback } from 'react'
import { api } from './api'

type Provider = 'bedrock' | 'copilot'

interface BedrockCreds {
  accessKeyId: string
  secretAccessKey: string
  region: string
  modelId: string
}

interface CopilotCreds {
  token: string
  modelId: string
}

const BEDROCK_DEFAULT: BedrockCreds = { accessKeyId: '', secretAccessKey: '', region: 'us-east-1', modelId: 'qwen3-32b' }
const COPILOT_DEFAULT: CopilotCreds = { token: '', modelId: 'gpt-4.1' }

export function LLMSettingsPage() {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [configured, setConfigured] = useState(false)

  const [provider, setProvider] = useState<Provider>('bedrock')
  const [bedrock, setBedrock] = useState<BedrockCreds>({ ...BEDROCK_DEFAULT })
  const [copilot, setCopilot] = useState<CopilotCreds>({ ...COPILOT_DEFAULT })

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await api.getLLMSettings()
      setProvider(data.provider as Provider)
      setConfigured(data.configured)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setError('')
    setSuccess('')
    try {
      const creds: Record<string, string> =
        provider === 'copilot'
          ? { token: copilot.token, modelId: copilot.modelId }
          : {
              accessKeyId: bedrock.accessKeyId,
              secretAccessKey: bedrock.secretAccessKey,
              region: bedrock.region,
              modelId: bedrock.modelId,
            }
      // Only include non-empty fields to avoid overwriting stored secrets with empty strings.
      const filteredCreds = Object.fromEntries(
        Object.entries(creds).filter(([, v]) => v.trim() !== '')
      )
      await api.saveLLMSettings(provider, filteredCreds)
      setSuccess('LLM settings saved.')
      setConfigured(true)
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <p className="loading-msg">Loading…</p>

  return (
    <div className="llm-settings-page">
      <h2 className="section-title">LLM Provider Settings</h2>
      <p className="section-subtitle">
        Choose your LLM provider and enter your own credentials.
        Secret values are never returned by the server — leave a field blank to keep the stored value.
      </p>

      {configured && <p className="status-badge configured">Provider configured ✓</p>}
      {error && <p className="error-msg">{error}</p>}
      {success && <p className="success-msg">{success}</p>}

      <form onSubmit={handleSave} className="llm-settings-form">
        <div className="form-group">
          <label htmlFor="provider-select">Provider</label>
          <select
            id="provider-select"
            value={provider}
            onChange={e => { setProvider(e.target.value as Provider); setSuccess(''); setError('') }}
          >
            <option value="bedrock">AWS Bedrock</option>
            <option value="copilot">GitHub Copilot</option>
          </select>
        </div>

        {provider === 'bedrock' && (
          <>
            <div className="form-group">
              <label>AWS Region</label>
              <input
                type="text"
                placeholder="us-east-1"
                value={bedrock.region}
                onChange={e => setBedrock(b => ({ ...b, region: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label>Model ID</label>
              <input
                type="text"
                placeholder="qwen3-32b"
                value={bedrock.modelId}
                onChange={e => setBedrock(b => ({ ...b, modelId: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label>Access Key ID</label>
              <input
                type="text"
                placeholder={configured ? '(stored — leave blank to keep)' : 'AKIA…'}
                value={bedrock.accessKeyId}
                onChange={e => setBedrock(b => ({ ...b, accessKeyId: e.target.value }))}
                autoComplete="off"
              />
            </div>
            <div className="form-group">
              <label>Secret Access Key</label>
              <input
                type="password"
                placeholder={configured ? '(stored — leave blank to keep)' : ''}
                value={bedrock.secretAccessKey}
                onChange={e => setBedrock(b => ({ ...b, secretAccessKey: e.target.value }))}
                autoComplete="new-password"
              />
            </div>
          </>
        )}

        {provider === 'copilot' && (
          <>
            <div className="form-group">
              <label>GitHub Token</label>
              <input
                type="password"
                placeholder={configured ? '(stored — leave blank to keep)' : 'github_pat_…'}
                value={copilot.token}
                onChange={e => setCopilot(c => ({ ...c, token: e.target.value }))}
                autoComplete="new-password"
              />
            </div>
            <div className="form-group">
              <label>Model ID</label>
              <input
                type="text"
                placeholder="gpt-4.1"
                value={copilot.modelId}
                onChange={e => setCopilot(c => ({ ...c, modelId: e.target.value }))}
              />
            </div>
          </>
        )}

        <button type="submit" className="btn-primary" disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </form>
    </div>
  )
}
