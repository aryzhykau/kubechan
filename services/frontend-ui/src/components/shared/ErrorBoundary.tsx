import React from 'react'
import { Alert, Box, Button } from '@mui/material'

interface Props {
  children: React.ReactNode
  fallback?: React.ReactNode
}

interface State { hasError: boolean; message: string }

export class ErrorBoundary extends React.Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, message: '' }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, message: error.message }
  }

  override render() {
    if (this.state.hasError) {
      return (
        this.props.fallback ?? (
          <Box sx={{ p: 4 }}>
            <Alert
              severity="error"
              action={
                <Button color="inherit" size="small" onClick={() => this.setState({ hasError: false, message: '' })}>
                  Retry
                </Button>
              }
            >
              Something went wrong: {this.state.message}
            </Alert>
          </Box>
        )
      )
    }
    return this.props.children
  }
}
