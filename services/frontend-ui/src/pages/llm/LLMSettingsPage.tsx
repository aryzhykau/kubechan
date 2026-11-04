import { useState, useEffect } from 'react'
import {
  Box, Typography, Alert, FormControl, InputLabel, Select, MenuItem,
  TextField, Button, Chip, CircularProgress, Divider, Paper,
  RadioGroup, FormControlLabel, Radio, Collapse,
} from '@mui/material'
import CheckCircleIcon from '@mui/icons-material/CheckCircle'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import ExpandLessIcon from '@mui/icons-material/ExpandLess'
import {
  useGetLLMSettingsQuery,
  useSaveLLMSettingsMutation,
  useGetLLMModelsQuery,
} from '../../store/api/adminApi'

type Provider   = 'bedrock' | 'copilot' | 'openai' | 'anthropic'
type AuthMethod = 'bearer'  | 'iam'

interface BedrockCreds {
  bearerToken: string; accessKeyId: string; secretAccessKey: string
  region: string; modelId: string
  thinkingBudget: string; maxTokens: string; temperature: string
}
interface CopilotCreds { token: string; modelId: string }

const BEDROCK_DEFAULT: BedrockCreds = {
  bearerToken: '', accessKeyId: '', secretAccessKey: '',
  region: 'us-east-1', modelId: 'qwen3-32b',
  thinkingBudget: '', maxTokens: '', temperature: '',
}
const COPILOT_DEFAULT: CopilotCreds = { token: '', modelId: 'gpt-4o' }

function credStr(fields: Record<string, unknown>, key: string): string {
  const v = fields[key]
  if (v == null || v === '***') return ''
  return String(v)
}

export function LLMSettingsPage() {
  const { data: settings, isLoading } = useGetLLMSettingsQuery()
  const { data: modelsData }          = useGetLLMModelsQuery()
  const [saveSettings, { isLoading: saving }] = useSaveLLMSettingsMutation()

  const [error, setError]       = useState('')
  const [success, setSuccess]   = useState('')
  const [configured, setConfigured] = useState(false)

  const [provider, setProvider]       = useState<Provider>('bedrock')
  const [authMethod, setAuthMethod]   = useState<AuthMethod>('bearer')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [bedrock, setBedrock]         = useState<BedrockCreds>({ ...BEDROCK_DEFAULT })
  const [copilot, setCopilot]         = useState<CopilotCreds>({ ...COPILOT_DEFAULT })
  const [apiKey, setApiKey] = useState('')
  const [apiModel, setApiModel] = useState('gpt-4.1')

  // Populate form when settings load
  useEffect(() => {
    if (!settings) return
    const p  = settings.provider as Provider
    const f  = (settings.credFields ?? {}) as Record<string, unknown>
    setProvider(p)
    setConfigured(settings.configured)
    setApiKey('')
    setApiModel(credStr(f, 'modelId') || (p === 'anthropic' ? 'claude-sonnet-4-6' : 'gpt-4.1'))

    if (p === 'bedrock') {
      const hasIam = 'accessKeyId' in f
      setAuthMethod(hasIam ? 'iam' : 'bearer')
      const updated: BedrockCreds = {
        ...BEDROCK_DEFAULT,
        region:         credStr(f, 'region')         || BEDROCK_DEFAULT.region,
        modelId:        credStr(f, 'modelId')        || BEDROCK_DEFAULT.modelId,
        thinkingBudget: credStr(f, 'thinkingBudget') || '',
        maxTokens:      credStr(f, 'maxTokens')      || '',
        temperature:    credStr(f, 'temperature')    || '',
        bearerToken: '', accessKeyId: '', secretAccessKey: '',
      }
      setBedrock(updated)
      setShowAdvanced(!!(updated.thinkingBudget || updated.maxTokens || updated.temperature))
    } else if (p === 'copilot') {
      setCopilot({ ...COPILOT_DEFAULT, modelId: credStr(f, 'modelId') || COPILOT_DEFAULT.modelId })
    }
  }, [settings])

  const models = modelsData?.providers ?? {}
  const copilotModels = (models['copilot'] ?? []) as { id: string; label: string }[]
  const bedrockModels = (models['bedrock'] ?? []) as { id: string; label: string }[]
  const openaiModels = (models['openai'] ?? []) as { id: string; label: string }[]
  const anthropicModels = (models['anthropic'] ?? []) as { id: string; label: string }[]
  const selectedModels = provider === 'bedrock' ? bedrockModels
    : provider === 'copilot' ? copilotModels
      : provider === 'openai' ? openaiModels : anthropicModels
  const selectedModel = provider === 'bedrock' ? bedrock.modelId
    : provider === 'copilot' ? copilot.modelId : apiModel
  const secretPlaceholder = configured && settings?.provider === provider ? '(stored — leave blank to keep)' : ''

  function updateModelId(value: string) {
    if (provider === 'bedrock') setBedrock(current => ({ ...current, modelId: value }))
    else if (provider === 'copilot') setCopilot(current => ({ ...current, modelId: value }))
    else setApiModel(value)
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setError(''); setSuccess('')
    const creds: Record<string, string> = {}
    if (provider === 'copilot') {
      creds.modelId = copilot.modelId
      if (copilot.token) creds.token = copilot.token
    } else if (provider === 'openai' || provider === 'anthropic') {
      creds.modelId = selectedModel
      if (apiKey) creds.apiKey = apiKey
    } else {
      creds.region  = bedrock.region
      creds.modelId = bedrock.modelId
      if (authMethod === 'bearer') {
        if (bedrock.bearerToken)     creds.bearerToken     = bedrock.bearerToken
      } else {
        if (bedrock.accessKeyId)     creds.accessKeyId     = bedrock.accessKeyId
        if (bedrock.secretAccessKey) creds.secretAccessKey = bedrock.secretAccessKey
      }
      if (bedrock.thinkingBudget) creds.thinkingBudget = bedrock.thinkingBudget
      if (bedrock.maxTokens)      creds.maxTokens      = bedrock.maxTokens
      if (bedrock.temperature)    creds.temperature    = bedrock.temperature
    }
    try {
      await saveSettings({ provider, credentials: creds }).unwrap()
      setSuccess('LLM settings saved.')
      setConfigured(true)
    } catch (e) {
      setError(String(e))
    }
  }

  if (isLoading) return (
    <Box sx={{ display: 'flex', justifyContent: 'center', mt: 6 }}><CircularProgress /></Box>
  )

  return (
    <Box sx={{ maxWidth: 560, mx: 'auto' }}>
      <Typography variant="h6" sx={{ fontWeight: 600, mb: 1 }}>LLM Provider Settings</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        Choose your LLM provider and enter your credentials.
        Secret fields are never returned — leave them blank to keep the stored value.
      </Typography>

      {configured && settings?.provider === provider && (
        <Chip icon={<CheckCircleIcon />} label="Provider configured" color="success" size="small" sx={{ mb: 2 }} />
      )}

      {error   && <Alert severity="error"   sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Paper elevation={0} sx={{ p: 3 }}>
        <Box component="form" onSubmit={handleSave} sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>

          <FormControl fullWidth size="small">
            <InputLabel>Provider</InputLabel>
            <Select value={provider} label="Provider" onChange={e => {
              const next = e.target.value as Provider
              setProvider(next)
              setApiKey('')
              if (next === 'openai') setApiModel('gpt-4.1')
              if (next === 'anthropic') setApiModel('claude-sonnet-4-6')
              setError(''); setSuccess('')
            }}>
              <MenuItem value="openai">OpenAI API</MenuItem>
              <MenuItem value="anthropic">Anthropic API</MenuItem>
              <MenuItem value="bedrock">AWS Bedrock</MenuItem>
              <MenuItem value="copilot">GitHub Copilot</MenuItem>
            </Select>
          </FormControl>

          <Divider />

          <TextField
            label="Model ID" size="small" value={selectedModel}
            onChange={e => updateModelId(e.target.value)} fullWidth
            helperText={selectedModels.length ? `Examples: ${selectedModels.slice(0, 4).map(m => m.id).join(', ')}. You can enter another model enabled for your account.` : 'Enter a model ID enabled for your account.'}
          />

          {provider === 'bedrock' && (
            <>
              <TextField label="AWS Region" size="small" placeholder="us-east-1" value={bedrock.region} onChange={e => setBedrock(b => ({ ...b, region: e.target.value }))} fullWidth />

              <Box>
                <Typography variant="caption" color="text.secondary" sx={{ mb: 0.5, display: 'block' }}>Authentication</Typography>
                <RadioGroup row value={authMethod} onChange={e => setAuthMethod(e.target.value as AuthMethod)}>
                  <FormControlLabel value="bearer" control={<Radio size="small" />} label="API Key" />
                  <FormControlLabel value="iam"    control={<Radio size="small" />} label="IAM Access Key" />
                </RadioGroup>
              </Box>

              {authMethod === 'bearer' && (
                <TextField label="Bedrock API Key" type="password" size="small" placeholder={secretPlaceholder || 'br-…'}
                  value={bedrock.bearerToken} onChange={e => setBedrock(b => ({ ...b, bearerToken: e.target.value }))} autoComplete="new-password" fullWidth />
              )}
              {authMethod === 'iam' && (
                <>
                  <TextField label="AWS Access Key ID" size="small" placeholder={secretPlaceholder || 'AKIA…'}
                    value={bedrock.accessKeyId} onChange={e => setBedrock(b => ({ ...b, accessKeyId: e.target.value }))} autoComplete="new-password" fullWidth />
                  <TextField label="AWS Secret Access Key" type="password" size="small" placeholder={secretPlaceholder}
                    value={bedrock.secretAccessKey} onChange={e => setBedrock(b => ({ ...b, secretAccessKey: e.target.value }))} autoComplete="new-password" fullWidth />
                </>
              )}

              <Box>
                <Button variant="text" size="small" onClick={() => setShowAdvanced(s => !s)}
                  startIcon={showAdvanced ? <ExpandLessIcon /> : <ExpandMoreIcon />}
                  sx={{ color: 'text.secondary', pl: 0, textTransform: 'none' }}>
                  Advanced settings
                </Button>
                <Collapse in={showAdvanced}>
                  <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 2 }}>
                    <TextField label="Thinking Budget (tokens)" size="small" type="number" fullWidth placeholder="0"
                      value={bedrock.thinkingBudget} onChange={e => setBedrock(b => ({ ...b, thinkingBudget: e.target.value }))}
                      helperText="Extended thinking tokens. 0 or blank = disabled." slotProps={{ htmlInput: { min: 0 } }} />
                    <TextField label="Max Output Tokens" size="small" type="number" fullWidth placeholder="4096"
                      value={bedrock.maxTokens} onChange={e => setBedrock(b => ({ ...b, maxTokens: e.target.value }))} slotProps={{ htmlInput: { min: 1 } }} />
                    <TextField label="Temperature" size="small" type="number" fullWidth placeholder="0.3"
                      value={bedrock.temperature} onChange={e => setBedrock(b => ({ ...b, temperature: e.target.value }))}
                      helperText="0.0 – 1.0" slotProps={{ htmlInput: { step: 0.05, min: 0, max: 1 } }} />
                  </Box>
                </Collapse>
              </Box>
            </>
          )}

          {provider === 'copilot' && (
            <>
              <TextField label="GitHub Token" type="password" size="small" placeholder={secretPlaceholder || 'github_pat_…'}
                value={copilot.token} onChange={e => setCopilot(c => ({ ...c, token: e.target.value }))} autoComplete="new-password" fullWidth />
            </>
          )}

          {(provider === 'openai' || provider === 'anthropic') && (
            <>
              <TextField
                label={provider === 'openai' ? 'OpenAI API key' : 'Anthropic API key'}
                type="password" size="small" placeholder={secretPlaceholder || (provider === 'openai' ? 'sk-…' : 'sk-ant-…')}
                value={apiKey} onChange={e => setApiKey(e.target.value)}
                autoComplete="new-password" required={!secretPlaceholder} fullWidth
              />
            </>
          )}

          <Button type="submit" variant="contained" disabled={saving} startIcon={saving ? <CircularProgress size={16} color="inherit" /> : undefined}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </Box>
      </Paper>
    </Box>
  )
}
