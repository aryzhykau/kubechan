# Phase 6 — User Management & Authentication

## Business Requirements (Confirmed)

### User Management
- On startup, if no admin user exists in the DB:
  - If K8s Secret `kubechan-admin-credentials` does **not** exist → generate a random password, create the Secret, create the admin user.
  - If K8s Secret **exists** → read the password from it, create the admin user using that password.
- Admin can create users (username, password, role).
- Roles: `admin`, `viewer`.
- All API endpoints require a valid JWT.

### Incident Visibility
- **Auto incidents** (cluster-watcher): visible to all authenticated users, no owner.
- **Manual incidents**: owned by the creating user; visible only to that user in the default view. Admins see all manual incidents but they are visually separated (labelled with owner username).

### Analysis Attribution
- Every `DiagnosticRun` triggered via the API records `triggered_by` (user ID).
- Attribution is displayed in the UI on the diagnostic run detail view.

---

## Prerequisites
- Phase 2B and 3B complete (backend-api fully operational).
- Phase 4 frontend exists (React/TypeScript, Vite).

---

## New Dependencies

| Package | Purpose |
|---|---|
| `golang-jwt/jwt/v5` | JWT sign / verify (Go) |
| `golang.org/x/crypto/bcrypt` | Password hashing (Go) |

No new frontend dependencies required (native `fetch` + existing state patterns).

---

## Tasks (ordered)

---

### [6.1] DB migration — users table (~30 min)
- File: `services/backend-api/db/migrations/006_users.sql`
```sql
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK(role IN ('admin', 'viewer')),
    created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

---

### [6.2] DB migration — user attribution (~30 min)
- File: `services/backend-api/db/migrations/007_user_attribution.sql`

```sql
-- Track which user triggered each analysis
ALTER TABLE analysis_requests ADD COLUMN triggered_by TEXT REFERENCES users(id);

-- Side-table for manual incident ownership (incidents live as K8s CRDs,
-- so ownership metadata is stored in SQLite alongside them)
CREATE TABLE manual_incident_owners (
    incident_id TEXT PRIMARY KEY,   -- matches the K8s Incident .metadata.name
    namespace   TEXT NOT NULL,
    owner_id    TEXT NOT NULL REFERENCES users(id),
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_mio_owner ON manual_incident_owners(owner_id);
```

---

### [6.3] Admin bootstrap at startup (~2h)
- File: `services/backend-api/startup/admin_bootstrap.go`
- Called from `main.go` after DB migrations, before serving HTTP.
- Logic:
  1. `SELECT COUNT(*) FROM users WHERE role = 'admin'` — if > 0, return immediately.
  2. Check K8s Secret `kubechan-admin-credentials` in `DEFAULT_NAMESPACE`.
     - If **not found**: generate 24-char random alphanumeric password → create Secret with key `password` → proceed to step 3.
     - If **found**: read `password` key from Secret data → proceed to step 3.
  3. Hash the password with bcrypt (cost 12) → `INSERT INTO users (id, username, password_hash, role) VALUES (uuid, 'admin', hash, 'admin')`.
- Secret structure:
  ```yaml
  apiVersion: v1
  kind: Secret
  metadata:
    name: kubechan-admin-credentials
    namespace: <DEFAULT_NAMESPACE>
  type: Opaque
  data:
    password: <base64>
  ```
- Dependencies injected: `*sql.DB`, `client.Client` (for K8s Secret API), `namespace string`, `*slog.Logger`.

---

### [6.4] Helm RBAC — Secret read/create permission (~30 min)
- File: `helm/kubechan/templates/backend-api/` — add or extend `Role` / `RoleBinding`.
- backend-api ServiceAccount needs:
  ```yaml
  rules:
    - apiGroups: [""]
      resources: ["secrets"]
      verbs: ["get", "create"]
      resourceNames: ["kubechan-admin-credentials"]
  ```
- Scope to a `Role` (namespaced), not `ClusterRole`, since the Secret is in the install namespace.

---

### [6.5] JWT middleware (~2h)
- File: `services/backend-api/handler/auth.go`
- JWT signing algorithm: `HS256`; secret from env var `JWT_SECRET` (min 32 bytes; fatal on startup if missing).
- Token lifetime: 24 hours (configurable via `JWT_TTL_HOURS` env var, default `24`).
- Token claims:
  ```json
  { "sub": "<user_id>", "username": "<username>", "role": "<role>", "exp": <unix> }
  ```
- Middleware `RequireAuth`:
  - Reads `Authorization: Bearer <token>` header.
  - Validates signature and expiry.
  - Injects `userID`, `username`, `role` into `context.Context` via typed keys.
  - Returns `401` on missing/invalid token, `403` on insufficient role.
- Helper `RequireAdmin`: wraps `RequireAuth` and additionally checks `role == "admin"`.
- Helper `UserFromCtx(ctx) (userID, username, role string)` for handlers to read identity.

---

### [6.6] Auth endpoints (~1h)
- File: `services/backend-api/handler/auth.go` (extend from [6.5])

**`POST /api/v1/auth/login`** — public (no auth middleware)
- Body: `{ "username": "...", "password": "..." }`
- `SELECT id, password_hash, role FROM users WHERE username = ?`
- `bcrypt.CompareHashAndPassword` — return `401` on mismatch.
- On success: sign JWT, return `{ "token": "...", "role": "...", "username": "..." }`.
- Use a constant-time compare path to avoid timing-based username enumeration.

**`GET /api/v1/auth/me`** — requires `RequireAuth`
- Returns `{ "userId": "...", "username": "...", "role": "..." }` from JWT context.
- Used by frontend on page load to validate an existing stored token.

---

### [6.7] User management endpoints — admin only (~1.5h)
- File: `services/backend-api/handler/users.go`

**`POST /api/v1/users`** — requires `RequireAdmin`
- Body: `{ "username": "...", "password": "...", "role": "admin|viewer" }`
- Validate: username non-empty, password min 8 chars, role in allowed set.
- Hash password (bcrypt cost 12), generate UUID, insert into `users`.
- Return `201` with `{ "id": "...", "username": "...", "role": "..." }`.
- Return `409` if username already taken.

**`GET /api/v1/users`** — requires `RequireAdmin`
- Returns array of `{ "id", "username", "role", "createdAt" }` (no password hashes).

**`DELETE /api/v1/users/{id}`** — requires `RequireAdmin`
- Prevent deleting the last admin user (check count before delete).
- Return `204` on success.

---

### [6.8] Apply auth middleware to all existing routes (~1h)
- File: `services/backend-api/main.go`
- Wrap the entire `/api/v1` subrouter with `RequireAuth` middleware.
- Exceptions (public):
  - `POST /api/v1/auth/login`
  - `/healthz`, `/readyz`, `/ws` (WebSocket — authenticate via token query param `?token=...` instead of header, checked inside `ws.ServeWS`).
- Admin-only routes wrapped with `RequireAdmin`:
  - `POST /api/v1/users`, `GET /api/v1/users`, `DELETE /api/v1/users/{id}`
  - `GET /api/v1/settings`, `PUT /api/v1/settings` (settings remain admin-only)

---

### [6.9] Manual incident ownership — create & enforce (~1.5h)
- File: `services/backend-api/handler/manual_incident.go`
- On `POST /api/v1/incidents/manual`:
  - Read `userID` from JWT context via `UserFromCtx`.
  - After creating the K8s Incident CRD, insert into `manual_incident_owners (incident_id, namespace, owner_id)`.
- File: `services/backend-api/handler/incidents.go`
- On `GET /api/v1/incidents`:
  - After fetching the CRD list, separate incidents into `auto` (no entry in `manual_incident_owners`) and `manual`.
  - For `manual` incidents, query `manual_incident_owners`:
    - If `role == "viewer"`: filter to only own incidents (`owner_id = userID`).
    - If `role == "admin"`: include all, but annotate each with `ownerUsername` field.
  - Response shape gains an optional field: `"ownerUsername": "alice"` (null for auto incidents).
- On `GET /api/v1/incidents/{id}` for a manual incident:
  - If viewer and not owner → `403`.
- On `POST /api/v1/incidents/{id}/analyze` and `/augment` and `/resolve` for a manual incident:
  - If viewer and not owner → `403`.

---

### [6.10] Analysis attribution — store triggered_by (~1h)
- File: `services/backend-api/handler/analysis.go`
- On `POST /api/v1/incidents/{id}/analyze`:
  - Read `userID` from JWT context.
  - Pass `triggered_by = userID` when inserting into `analysis_requests`.
- File: `services/backend-api/handler/diagnosticruns.go`
- On `GET /api/v1/diagnosticruns/{id}`:
  - Join `analysis_requests` to include `triggered_by` user ID in response.
  - Resolve to `triggeredByUsername` via a `SELECT username FROM users WHERE id = ?`.
  - Add `"triggeredBy": { "userId": "...", "username": "..." }` to the response payload.

---

### [6.11] Frontend — Login page (~2h)
- File: `services/frontend-ui/src/LoginPage.tsx` (new)
- Simple form: username + password fields, submit button.
- On submit: `POST /api/v1/auth/login` → store JWT in `localStorage` (`kubechan_token`).
- On success: navigate to main app.
- On `401`: show "Invalid credentials" inline error.
- File: `services/frontend-ui/src/api.ts` (modify)
  - Add `Authorization: Bearer <token>` header to all requests using stored token.
  - On any `401` response from the API: clear token and redirect to login.
- File: `services/frontend-ui/src/App.tsx` (modify)
  - On mount: call `GET /api/v1/auth/me` with stored token.
  - If valid → render app normally, expose `currentUser` via React context.
  - If invalid/missing → render `<LoginPage />`.

---

### [6.12] Frontend — Manual incident visibility (~1h)
- File: `services/frontend-ui/src/IncidentList.tsx` (modify)
- Auto incidents and manual incidents already rendered; extend to:
  - For viewer: manual incidents list only shows own incidents (server already filters).
  - For admin: render a visual separator or label (`"by <ownerUsername>"`) on manual incidents owned by others.
- File: `services/frontend-ui/src/ManualIncidentModal.tsx` — no changes needed (server now enforces ownership on create).

---

### [6.13] Frontend — Analysis attribution display (~30 min)
- File: `services/frontend-ui/src/DiagnosticRunDetail.tsx` (modify)
- If `triggeredBy` field is present in the diagnostic run response, render:
  ```
  Analysis triggered by: <username>
  ```
  in the run metadata section.

---

### [6.14] Frontend — User management UI (admin only) (~2h)
- File: `services/frontend-ui/src/UsersPage.tsx` (new)
- Only rendered when `currentUser.role === 'admin'`.
- Shows table of existing users (username, role, created date).
- "Add user" form: username, password, role dropdown.
- Delete button per user (with confirmation), disabled if it would remove the last admin.
- Accessible from a sidebar link visible only to admins.
- File: `services/frontend-ui/src/KubeChanSidebar.tsx` (modify) — add Users nav item for admins.

---

### [6.15] WebSocket authentication (~30 min)
- File: `services/backend-api/ws/hub.go` or `services/backend-api/main.go`
- WebSocket upgrade endpoint `/ws` currently has no auth.
- Accept token via query param: `GET /ws?token=<jwt>`.
- Validate token before upgrading; reject with `401` if invalid.
- File: `services/frontend-ui/src/useWebSocket.ts` (modify)
  - Append `?token=<stored_jwt>` to the WebSocket URL.

---

## Env vars added

| Var | Required | Default | Description |
|---|---|---|---|
| `JWT_SECRET` | Yes (fatal) | — | HS256 signing key, min 32 bytes |
| `JWT_TTL_HOURS` | No | `24` | Token lifetime in hours |

---

## Helm chart changes summary
- `backend-api` Deployment: add `JWT_SECRET` from a K8s Secret ref (new Secret `kubechan-jwt-secret` created by Helm if not present, or supplied by user).
- `backend-api` Role: add `get`/`create` on `secrets` for `kubechan-admin-credentials`.
- `values.yaml`: add `auth.jwtSecretName` (default `kubechan-jwt-secret`) and `auth.adminSecretName` (default `kubechan-admin-credentials`).

---

## Task order for implementation

```
6.1 → 6.2 → 6.3 → 6.4   (DB + bootstrap, must come first)
6.5 → 6.6 → 6.7 → 6.8   (auth layer)
6.9 → 6.10               (ownership + attribution, depends on auth layer)
6.11 → 6.12 → 6.13 → 6.14 → 6.15  (frontend, depends on API)
```
