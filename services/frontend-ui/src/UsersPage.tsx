import { useState, useEffect, useCallback } from 'react'
import { api } from './api'

interface User {
  id: string
  username: string
  role: string
  createdAt: string
}

export function UsersPage() {
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // New user form
  const [newUsername, setNewUsername] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newRole, setNewRole] = useState<'admin' | 'viewer'>('viewer')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')

  const [deletingId, setDeletingId] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await api.listUsers()
      setUsers(data)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    setCreateError('')
    setCreating(true)
    try {
      await api.createUser(newUsername, newPassword, newRole)
      setNewUsername('')
      setNewPassword('')
      setNewRole('viewer')
      await load()
    } catch (e) {
      setCreateError(String(e))
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(id: string, username: string) {
    if (!confirm(`Delete user "${username}"? This cannot be undone.`)) return
    setDeletingId(id)
    try {
      await api.deleteUser(id)
      await load()
    } catch (e) {
      setError(String(e))
    } finally {
      setDeletingId(null)
    }
  }

  return (
    <div className="users-page">
      <h2 className="section-title">User Management</h2>

      {error && <p className="error-msg">{error}</p>}

      <table className="users-table">
        <thead>
          <tr>
            <th>Username</th>
            <th>Role</th>
            <th>Created</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {loading && (
            <tr><td colSpan={4} className="table-empty">Loading…</td></tr>
          )}
          {!loading && users.length === 0 && (
            <tr><td colSpan={4} className="table-empty">No users found.</td></tr>
          )}
          {users.map(u => (
            <tr key={u.id}>
              <td>{u.username}</td>
              <td><span className={`role-badge role-${u.role}`}>{u.role}</span></td>
              <td>{new Date(u.createdAt).toLocaleDateString()}</td>
              <td>
                <button
                  className="btn-danger-sm"
                  disabled={deletingId === u.id}
                  onClick={() => handleDelete(u.id, u.username)}
                >
                  {deletingId === u.id ? '…' : 'Delete'}
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="create-user-form-wrapper">
        <h3 className="section-subtitle">Add User</h3>
        <form className="create-user-form" onSubmit={handleCreate}>
          <input
            className="form-input"
            type="text"
            placeholder="Username"
            value={newUsername}
            onChange={e => setNewUsername(e.target.value)}
            required
            disabled={creating}
          />
          <input
            className="form-input"
            type="password"
            placeholder="Password (min 8 chars)"
            value={newPassword}
            onChange={e => setNewPassword(e.target.value)}
            required
            minLength={8}
            disabled={creating}
          />
          <select
            className="form-select"
            value={newRole}
            onChange={e => setNewRole(e.target.value as 'admin' | 'viewer')}
            disabled={creating}
          >
            <option value="viewer">Viewer</option>
            <option value="admin">Admin</option>
          </select>
          <button className="btn-primary" type="submit" disabled={creating}>
            {creating ? 'Creating…' : 'Create User'}
          </button>
          {createError && <p className="error-msg">{createError}</p>}
        </form>
      </div>
    </div>
  )
}
