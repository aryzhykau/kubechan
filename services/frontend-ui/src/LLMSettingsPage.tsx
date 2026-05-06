import { useState, useEffect, useCallback } from 'react'
import {
  Box,
  Typography,
  Alert,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  TextField,
  Button,
  Chip,
  CircularProgress,
  Divider,
  Paper,
  ThemeProvider,
  createTheme,
  RadioGroup,
  FormControlLabel,
  Radio,
  Collapse,
} from '@mui/material'
import CheckCircleIcon from '@mui/icons-material/CheckCircle'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import ExpandLessIcon from '@mui/icons-material/ExpandLess'
import { api } from './api'

type Provider = 'bedrock' | 'copilot'
type AuthMethod = 'bearer' | 'iam'

interface BedrockCreds {
  // auth — only one group should be filled
  bearerToken: string
  accessKeyId: string
  secretAccessKey: string
  // required
  region: string
  modelId: string
  // advanced (optional — gateway uses built-in defaults if absent)
  thinkingBudget: string
  maxTokens: string
  temperature: string
}

interface CopilotCreds {
  token: string
  modelId: string
}

interface ModelEntry {
  id: string
  label: string
}

const BEDROCK_DEFAULT: BedrockCreds = {
  bearerToken: '',
  accessKeyId: '',
  secretAccessKey: '',
  region: 'us-east-1',
  modelId: 'qwen3-32b',
  thinkingBudget: '',
  maxTokens: '',
  temperature: '',
}
const COPILOT_DEFAULT: CopilotCreds = { token: '', modelId: 'gpt-5.4' }

/** Return the string value of a credFields entry, or '' if missing/masked. */
function credStr(fields: Record<string, unknown>, key: string): string {
  const v = fields[key]
  if (v == null || v === '***') return ''
  return String(v)
}

const pageTheme = createTheme({
  palette: {
    mode: 'dark',
    background: { paper: '#16162a', default: '#0f0f1e' },
    primary:    { main: '#6366f1', light: '#818cf8', dark: '#4f46e5' },
    secondary:  { main: '#f59e0b' },
    error:      { main: '#f87171' },
    success:    { main: '#34d399' },
    divider:    '#2a2a42',
  },
})

export function LLMSettingsPage() {
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [configured, setConfigured] = useState(false)

  const [provider, setProvider] = useState<Provider>('bedrock')
  const [authMethod, setAuthMethod] = useState<AuthMethod>('bearer')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [bedrock, setBedrock] = useState<BedrockCreds>({ ...BEDROCK_DEFAULT })
  const [copilot, setCopilot] = useState<CopilotCreds>({ ...COPILOT_DEFAULT })
  const [models, setModels] = useState<Record<string, ModelEntry[]>>({})

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const [settings, modelsData] = await Promise.all([
        api.getLLMSettings(),
        api.getLLMModels(),
      ])
      const p = settings.provider as Provider
      const f = settings.credFields || {}
      setProvider(p)
      setConfigured(settings.configured)
      setModels(modelsData.providers)

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
          // secret fields always left blank — user re-enters to replace
          bearerToken: '', accessKeyId: '', secretAccessKey: '',
        }
        setBedrock(updated)
        setShowAdvanced(!!(updated.thinkingBudget || updated.maxTokens || updated.temperature))
      } else if (p === 'copilot') {
        setCopilot({
          ...COPILOT_DEFAULT,
          modelId: credStr(f, 'modelId') || COPILOT_DEFAULT.modelId,
        })
      }
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
      const creds: Record<string, string> = {}

      if (provider === 'copilot') {
        creds.modelId = copilot.modelId
        if (copilot.token) creds.token = copilot.token
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

      await api.saveLLMSettings(provider, creds)
      setSuccess('LLM settings saved.')
      setConfigured(true)
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  if (loading) return (
    <Box sx={{ display: 'flex', justifyContent: 'center', mt: 6 }}>
      <CircularProgress />
    </Box>
  )

  const copilotModels = models['copilot'] ?? []
  const bedrockModels = models['bedrock'] ?? []
  const secretPlaceholder = configured ? '(stored — leave blank to keep)' : ''

  return (
    <ThemeProvider theme={pageTheme}>
      <Box sx={{ maxWidth: 560, mx: 'auto', mt: 4, px: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 600, mb: 1 }}>
          LLM Provider Settings
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Choose your LLM provider and enter your credentials.
          Secret fields are never returned — leave them blank to keep the stored value.
        </Typography>

        {configured && (
          <Chip
            icon={<CheckCircleIcon />}
            label="Provider configured"
            color="success"
            size="small"
            sx={{ mb: 2 }}
          />
        )}

        {error   && <Alert severity="error"   sx={{ mb: 2 }}>{error}</Alert>}
        {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

        <Paper variant="outlined" sx={{ p: 3, bgcolor: 'background.paper' }}>
          <Box component="form" onSubmit={handleSave} sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>

            {/* ── Provider selector ── */}
            <FormControl fullWidth size="small">
              <InputLabel>Provider</InputLabel>
              <Select
                value={provider}
                label="Provider"
                onChange={e => {
                  setProvider(e.target.value as Provider)
                  setSuccess('')
                  setError('')
                }}
              >
                <MenuItem value="bedrock">AWS Bedrock</MenuItem>
                <MenuItem value="copilot">GitHub Copilot</MenuItem>
              </Select>
            </FormControl>

            <Divider />

            {/* ── Bedrock fields ── */}
            {provider === 'bedrock' && (
              <>
                <TextField
                  label="AWS Region"
                  size="small"
                  placeholder="us-east-1"
                  value={bedrock.region}
                  onChange={e => setBedrock(b => ({ ...b, region: e.target.value }))}
                  fullWidth
                />

                <FormControl fullWidth size="small">
                  <InputLabel>Model</InputLabel>
                  <Select
                    value={bedrock.modelId}
                    label="Model"
                    onChange={e => setBedrock(b => ({ ...b, modelId: e.target.value }))}
                  >
                    {bedrockModels.map(m => (
                      <MenuItem key={m.id} value={m.id}>{m.label}</MenuItem>
                    ))}
                    {bedrockModels.length === 0 && (
                      <MenuItem value={bedrock.modelId}>{bedrock.modelId}</MenuItem>
                    )}
                  </Select>
                </FormControl>

                {/* ── Authentication method ── */}
                <Box>
                  <Typography variant="caption" color="text.secondary" sx={{ mb: 0.5, display: 'block' }}>
                    Authentication
                  </Typography>
                  <RadioGroup
                    row
                    value={authMethod}
                    onChange={e => setAuthMethod(e.target.value as AuthMethod)}
                  >
                    <FormControlLabel value="bearer" control={<Radio size="small" />} label="API Key" />
                    <FormControlLabel value="iam"    control={<Radio size="small" />} label="IAM Access Key" />
                  </RadioGroup>
                </Box>

                {authMethod === 'bearer' && (
                  <TextField
                    label="Bedrock API Key"
                    type="password"
                    size="small"
                    placeholder={secretPlaceholder || 'br-…'}
                    value={bedrock.bearerToken}
                    onChange={e => setBedrock(b => ({ ...b, bearerToken: e.target.value }))}
                    autoComplete="new-password"
                    fullWidth
                  />
                )}

                {authMethod === 'iam' && (
                  <>
                    <TextField
                      label="AWS Access Key ID"
                      size="small"
                      placeholder={secretPlaceholder || 'AKIA…'}
                      value={bedrock.accessKeyId}
                      onChange={e => setBedrock(b => ({ ...b, accessKeyId: e.target.value }))}
                      autoComplete="new-password"
                      fullWidth
                    />
                    <TextField
                      label="AWS Secret Access Key"
                      type="password"
                      size="small"
                      placeholder={secretPlaceholder}
                      value={bedrock.secretAccessKey}
                      onChange={e => setBedrock(b => ({ ...b, secretAccessKey: e.target.value }))}
                      autoComplete="new-password"
                      fullWidth
                    />
                  </>
                )}

                {/* ── Advanced ── */}
                <Box>
                  <Button
                    variant="text"
                    size="small"
                    onClick={() => setShowAdvanced(s => !s)}
                    startIcon={showAdvanced ? <ExpandLessIcon /> : <ExpandMoreIcon />}
                    sx={{ color: 'text.secondary', pl: 0, textTransform: 'none' }}
                  >
                    Advanced settings
                  </Button>
                  <Collapse in={showAdvanced}>
                    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 2 }}>
                      <TextField
                        label="Thinking Budget (tokens)"
                        size="small"
                        type="number"
                        fullWidth
                        placeholder="0"
                        value={bedrock.thinkingBudget}
                        onChange={e => setBedrock(b => ({ ...b, thinkingBudget: e.target.value }))}
                        helperText="Extended thinking tokens. 0 or blank = disabled."
                        slotProps={{ htmlInput: { min: 0 } }}
                      />
                      <TextField
                        label="Max Output Tokens"
                        size="small"
                        type="number"
                        fullWidth
                        placeholder="4096"
                        value={bedrock.maxTokens}
                        onChange={e => setBedrock(b => ({ ...b, maxTokens: e.target.value }))}
                        slotProps={{ htmlInput: { min: 1 } }}
                      />
                      <TextField
                        label="Temperature"
                        size="small"
                        type="number"
                        fullWidth
                        placeholder="0.3"
                        value={bedrock.temperature}
                        onChange={e => setBedrock(b => ({ ...b, temperature: e.target.value }))}
                        helperText="0.0 – 1.0"
                        slotProps={{ htmlInput: { step: 0.05, min: 0, max: 1 } }}
                      />
                    </Box>
                  </Collapse>
                </Box>
              </>
            )}

            {/* ── Copilot fields ── */}
            {provider === 'copilot' && (
              <>
                <TextField
                  label="GitHub Token"
                  type="password"
                  size="small"
                  placeholder={secretPlaceholder || 'github_pat_…'}
                  value={copilot.token}
                  onChange={e => setCopilot(c => ({ ...c, token: e.target.value }))}
                  autoComplete="new-password"
                  fullWidth
                />
                <FormControl fullWidth size="small">
                  <InputLabel>Model</InputLabel>
                  <Select
                    value={copilot.modelId}
                    label="Model"
                    onChange={e => setCopilot(c => ({ ...c, modelId: e.target.value }))}
                  >
                    {copilotModels.map(m => (
                      <MenuItem key={m.id} value={m.id}>{m.label}</MenuItem>
                    ))}
                    {copilotModels.length === 0 && (
                      <MenuItem value={copilot.modelId}>{copilot.modelId}</MenuItem>
                    )}
                  </Select>
                </FormControl>
              </>
            )}

            <Button
              type="submit"
              variant="contained"
              disabled={saving}
              startIcon={saving ? <CircularProgress size={16} /> : undefined}
            >
              {saving ? 'Saving…' : 'Save'}
            </Button>
          </Box>
        </Paper>
      </Box>
    </ThemeProvider>
  )
}
