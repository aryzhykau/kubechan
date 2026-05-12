import { useState } from 'react'
import {
  Box, Typography, Alert, Button, TextField, Select, MenuItem,
  FormControl, InputLabel, Table, TableHead, TableBody, TableRow, TableCell,
  Chip, IconButton, Skeleton, Stack,
  Dialog, DialogTitle, DialogContent, DialogContentText, DialogActions,
} from '@mui/material'
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutlined'
import PersonAddIcon from '@mui/icons-material/PersonAdd'
import { useListUsersQuery, useCreateUserMutation, useDeleteUserMutation } from '../../store/api/adminApi'

export function UsersPage() {
  const { data: users = [], isLoading, isError, error } = useListUsersQuery()
  const [createUser, { isLoading: creating }] = useCreateUserMutation()
  const [deleteUser, { isLoading: deletingId }] = useDeleteUserMutation()

  const [newUsername, setNewUsername] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newRole, setNewRole] = useState<'admin' | 'viewer'>('viewer')
  const [createError, setCreateError] = useState('')

  const [confirmDelete, setConfirmDelete] = useState<{ id: string; username: string } | null>(null)

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    setCreateError('')
    try {
      await createUser({ username: newUsername, password: newPassword, role: newRole }).unwrap()
      setNewUsername('')
      setNewPassword('')
      setNewRole('viewer')
    } catch (e) {
      setCreateError(String(e))
    }
  }

  return (
    <Box>
      <Typography variant="h6" sx={{ mb: 3 }}>User Management</Typography>

      {/* Create user form */}
      <Box
        component="form"
        onSubmit={handleCreate}
        sx={{
          background: '#161923',
          border: '1px solid #2a2f4a',
          borderRadius: '12px',
          p: 2.5,
          mb: 3,
        }}
      >
        <Typography variant="subtitle2" sx={{ mb: 2, fontWeight: 700 }}>New User</Typography>
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5} sx={{ alignItems: 'flex-start' }}>
          <TextField
            label="Username"
            value={newUsername}
            onChange={e => setNewUsername(e.target.value)}
            required
            size="small"
            disabled={creating}
            sx={{ flex: 1 }}
          />
          <TextField
            label="Password"
            type="password"
            value={newPassword}
            onChange={e => setNewPassword(e.target.value)}
            required
            size="small"
            disabled={creating}
            sx={{ flex: 1 }}
          />
          <FormControl size="small" sx={{ minWidth: 120 }}>
            <InputLabel>Role</InputLabel>
            <Select
              value={newRole}
              label="Role"
              onChange={e => setNewRole(e.target.value as 'admin' | 'viewer')}
              disabled={creating}
            >
              <MenuItem value="viewer">viewer</MenuItem>
              <MenuItem value="admin">admin</MenuItem>
            </Select>
          </FormControl>
          <Button
            type="submit"
            variant="contained"
            size="small"
            disabled={creating}
            startIcon={<PersonAddIcon />}
            sx={{ whiteSpace: 'nowrap', height: 40 }}
          >
            {creating ? 'Creating…' : 'Add User'}
          </Button>
        </Stack>
        {createError && <Alert severity="error" sx={{ mt: 1.5 }}>{createError}</Alert>}
      </Box>

      {/* Users table */}
      {isError && (
        <Alert severity="error" sx={{ mb: 2 }}>Failed to load users: {String(error)}</Alert>
      )}

      <Box sx={{ background: '#161923', border: '1px solid #2a2f4a', borderRadius: '12px', overflow: 'hidden' }}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Username</TableCell>
              <TableCell>Role</TableCell>
              <TableCell>Created</TableCell>
              <TableCell align="right" />
            </TableRow>
          </TableHead>
          <TableBody>
            {isLoading && (
              [1, 2, 3].map(i => (
                <TableRow key={i}>
                  <TableCell colSpan={4}><Skeleton height={32} /></TableCell>
                </TableRow>
              ))
            )}
            {!isLoading && users.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} sx={{ textAlign: 'center', py: 4, color: 'text.secondary' }}>No users found.</TableCell>
              </TableRow>
            )}
            {users.map(u => (
              <TableRow key={u.id}>
                <TableCell sx={{ fontWeight: 600 }}>{u.username}</TableCell>
                <TableCell>
                  <Chip
                    label={u.role}
                    size="small"
                    color={u.role === 'admin' ? 'primary' : 'default'}
                    variant={u.role === 'admin' ? 'filled' : 'outlined'}
                    sx={{ fontSize: '0.7rem', height: 20 }}
                  />
                </TableCell>
                <TableCell sx={{ color: 'text.secondary', fontSize: '0.82rem' }}>
                  {new Date(u.createdAt).toLocaleDateString()}
                </TableCell>
                <TableCell align="right">
                  <IconButton
                    size="small"
                    color="error"
                    disabled={!!deletingId}
                    onClick={() => setConfirmDelete({ id: u.id, username: u.username })}
                    sx={{ opacity: 0.6, '&:hover': { opacity: 1 } }}
                  >
                    <DeleteOutlineIcon fontSize="small" />
                  </IconButton>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Box>

      {/* Confirm delete dialog */}
      <Dialog open={!!confirmDelete} onClose={() => setConfirmDelete(null)} maxWidth="xs" fullWidth>
        <DialogTitle>Delete user?</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Delete <strong>{confirmDelete?.username}</strong>? This cannot be undone.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmDelete(null)}>Cancel</Button>
          <Button
            variant="contained"
            color="error"
            onClick={async () => {
              if (!confirmDelete) return
              setConfirmDelete(null)
              await deleteUser(confirmDelete.id)
            }}
          >
            Delete
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  )
}
