import { useState } from 'react'
import {
  Box, Card, CardContent, TextField, Button, Alert,
  Typography, CircularProgress, InputAdornment, IconButton,
} from '@mui/material'
import VisibilityIcon from '@mui/icons-material/Visibility'
import VisibilityOffIcon from '@mui/icons-material/VisibilityOff'
import { useNavigate } from 'react-router-dom'
import { useLoginMutation } from '../../store/api/authApi'
import { useAppDispatch } from '../../store/hooks'
import { setUser } from '../../store/slices/authSlice'
import type { CurrentUser } from '../../api/index'

export function LoginPage() {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const [loginApi, { isLoading }] = useLoginMutation()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPw, setShowPw] = useState(false)
  const [error, setError] = useState('')

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    try {
      const res = await loginApi({ username, password }).unwrap()
      // Derive CurrentUser from login response
      const user: CurrentUser = {
        userId: '',
        username: res.username,
        role: res.role as 'admin' | 'viewer',
      }
      dispatch(setUser(user))
      navigate('/', { replace: true })
    } catch {
      setError('Invalid username or password.')
    }
  }

  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'flex',
        background: '#0d0f17',
      }}
    >
      {/* Left panel — form */}
      <Box
        sx={{
          flex: 1,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          p: 4,
        }}
      >
        <Card
          sx={{
            width: '100%',
            maxWidth: 400,
            p: 1,
            background: 'transparent',
            border: 'none',
            boxShadow: 'none',
          }}
        >
          <CardContent sx={{ p: 4 }}>
            {/* Logo */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 4 }}>
              <Box
                component="img"
                src="/logo.png"
                alt="KubeChan"
                sx={{ width: 36, height: 36, objectFit: 'contain' }}
                onError={(e) => {
                  (e.target as HTMLImageElement).style.display = 'none'
                }}
              />
              <Typography variant="h5" sx={{ fontWeight: 800, letterSpacing: '-0.03em' }}>
                KubeChan
              </Typography>
            </Box>

            <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
              Sign in to your account
            </Typography>

            <Box component="form" onSubmit={handleSubmit} sx={{ display: 'flex', flexDirection: 'column', gap: 2.5 }}>
              <TextField
                label="Username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                required
                disabled={isLoading}
                fullWidth
                size="small"
              />
              <TextField
                label="Password"
                type={showPw ? 'text' : 'password'}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
                disabled={isLoading}
                fullWidth
                size="small"
                slotProps={{
                  input: {
                    endAdornment: (
                      <InputAdornment position="end">
                        <IconButton size="small" onClick={() => setShowPw(!showPw)} edge="end" tabIndex={-1}>
                          {showPw ? <VisibilityOffIcon fontSize="small" /> : <VisibilityIcon fontSize="small" />}
                        </IconButton>
                      </InputAdornment>
                    ),
                  },
                }}
              />

              {error && <Alert severity="error" sx={{ py: 0.5 }}>{error}</Alert>}

              <Button
                type="submit"
                variant="contained"
                size="large"
                disabled={isLoading}
                fullWidth
                sx={{ mt: 0.5 }}
              >
                {isLoading ? <CircularProgress size={20} color="inherit" /> : 'Sign in'}
              </Button>
            </Box>
          </CardContent>
        </Card>
      </Box>

      {/* Right panel — KubeChan character */}
      <Box
        sx={{
          flex: 1,
          display: { xs: 'none', md: 'flex' },
          alignItems: 'flex-end',
          justifyContent: 'center',
          overflow: 'hidden',
        }}
      >
        <Box
          component="img"
          src="/kubechan-idle-1.png"
          alt="KubeChan"
          sx={{ width: '75%', maxWidth: 520, objectFit: 'contain', userSelect: 'none', pointerEvents: 'none' }}
        />
      </Box>
    </Box>
  )
}
