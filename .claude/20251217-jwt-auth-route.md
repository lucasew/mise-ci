# JWT Authentication System for mise-ci

## Overview
Implement a JWT-based authentication system with two access levels:
1. **Pre-authenticated links** - Permanent tokens embedded in URLs for viewing specific run details (shared via GitHub PR status)
2. **Admin access** - HTTP Basic Auth for viewing all runs (username/password from env vars)

## User Requirements
- Token passed in URL query parameters (e.g., `?token=...`)
- No cookies or localStorage
- Pre-auth tokens grant access to ALL endpoints for that specific run
- Tokens are permanent (no expiration)
- Admin credentials hardcoded from environment variables

## Implementation Steps

### 1. Enhanced JWT Token System

**File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/core/core.go`**

#### 1.1 Add token type constants
```go
const (
    TokenTypeWorker = "worker"
    TokenTypeUI     = "ui"
)
```

#### 1.2 Create separate token generation methods
- `GenerateWorkerToken(runID)` - For worker authentication (keep existing behavior)
- `GenerateUIToken(runID)` - New method for permanent UI access tokens (no expiration)
- Both use HMAC-SHA256 signing with `type` claim to differentiate

#### 1.3 Update `ValidateToken` to return token type
- Return `(runID string, tokenType string, error)`
- Add backwards compatibility for tokens without `type` claim

#### 1.4 Add `UIToken` field to `RunInfo` struct
```go
type RunInfo struct {
    ID         string
    Status     RunStatus
    StartedAt  time.Time
    FinishedAt *time.Time
    Logs       []LogEntry
    ExitCode   *int32
    UIToken    string    // NEW
}
```

#### 1.5 Update `CreateRun` to generate UI token
- Generate permanent UI token when run is created
- Store in `RunInfo.UIToken`

#### 1.6 Add helper method `GetRunUIURL`
```go
func (c *Core) GetRunUIURL(runID string, baseURL string) string
```
- Returns full URL with token: `https://example.com/ui/run/<runID>?token=...`

### 2. Authentication Middleware

**New File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/server/auth.go`**

#### 2.1 Create `AuthConfig` struct
```go
type AuthConfig struct {
    AdminUsername string
    AdminPassword string
}
```

#### 2.2 Create `AuthMiddleware` struct
```go
type AuthMiddleware struct {
    core       *core.Core
    authConfig *AuthConfig
}
```

#### 2.3 Implement helper functions
- `extractTokenFromQuery(r *http.Request) string` - Get token from URL query param
- `extractRunIDFromPath(path string) string` - Parse run ID from path

#### 2.4 Implement middleware methods

**`RequireRunToken(next http.HandlerFunc) http.HandlerFunc`**
- Extract token from query parameter
- Validate token is UI type
- Extract run ID from path
- Verify token's run_id matches path's run_id
- Return 401/403 if invalid

**`RequireBasicAuth(next http.HandlerFunc) http.HandlerFunc`**
- Check HTTP Basic Auth credentials
- Use `subtle.ConstantTimeCompare` to prevent timing attacks
- If no credentials configured, allow access (graceful degradation)
- Return 401 with WWW-Authenticate header if invalid

**`RequireStatusStreamAuth(next http.HandlerFunc) http.HandlerFunc`**
- Accept EITHER valid UI token OR Basic Auth
- Try token first, fall back to Basic Auth
- Needed for SSE status stream that can be accessed from both contexts

### 3. Configuration Updates

**File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/config/config.go`**

#### 3.1 Add `AuthConfig` to main `Config` struct
```go
type Config struct {
    Server ServerConfig `yaml:"server" mapstructure:"server"`
    JWT    JWTConfig    `yaml:"jwt" mapstructure:"jwt"`
    GitHub GitHubConfig `yaml:"github" mapstructure:"github"`
    Nomad  NomadConfig  `yaml:"nomad" mapstructure:"nomad"`
    Auth   AuthConfig   `yaml:"auth" mapstructure:"auth"`  // NEW
}
```

#### 3.2 Define `AuthConfig` struct
```go
type AuthConfig struct {
    AdminUsername string `yaml:"admin_username" mapstructure:"admin_username"`
    AdminPassword string `yaml:"admin_password" mapstructure:"admin_password"`
}
```

**File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/cmd/mise-ci/main.go`**

#### 3.3 Bind environment variables
```go
viper.BindEnv("auth.admin_username")  // MISE_CI_AUTH_ADMIN_USERNAME
viper.BindEnv("auth.admin_password")  // MISE_CI_AUTH_ADMIN_PASSWORD
```

### 4. HTTP Server Integration

**File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/server/http.go`**

#### 4.1 Add `authMiddleware` field to `HttpServer`
```go
type HttpServer struct {
    addr           string
    service        *core.Service
    wsServer       *WebSocketServer
    uiServer       *UIServer
    authMiddleware *AuthMiddleware  // NEW
    logger         *slog.Logger
}
```

#### 4.2 Update `NewHttpServer` constructor
- Accept `authMiddleware *AuthMiddleware` parameter

#### 4.3 Apply middleware to routes in `Serve()` method
```go
// Protected with Basic Auth (admin only)
mux.HandleFunc("/ui/", s.authMiddleware.RequireBasicAuth(s.uiServer.HandleIndex))

// Protected with run-specific token
mux.HandleFunc("/ui/run/", s.authMiddleware.RequireRunToken(s.uiServer.HandleRun))
mux.HandleFunc("/ui/logs/", s.authMiddleware.RequireRunToken(s.uiServer.HandleLogs))

// Protected with either token or Basic Auth
mux.HandleFunc("/ui/status-stream", s.authMiddleware.RequireStatusStreamAuth(s.uiServer.HandleStatusStream))
```

### 5. Service Integration - GitHub PR Status URLs

**File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/core/service.go`**

#### 5.1 Update `StartRun` method
- Create run first (generates UI token)
- Build pre-authenticated URL using `core.GetRunUIURL()`
- Include URL in all GitHub status updates (pending, success, failure)

```go
run := s.Core.CreateRun(runID)

publicURL := s.Config.Server.PublicURL
if publicURL == "" {
    publicURL = s.Config.Server.HTTPAddr
}
targetURL := s.Core.GetRunUIURL(runID, publicURL)

status := forge.Status{
    State:       forge.StatePending,
    Context:     "mise-ci",
    Description: "Run started",
    TargetURL:   targetURL,  // Pre-authenticated link
}
```

### 6. UI Template Updates

**File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/server/templates/pages/run.html`**

#### 6.1 Update JavaScript to propagate token to SSE endpoints
```javascript
// Extract token from URL
const urlParams = new URLSearchParams(window.location.search);
const token = urlParams.get('token');

// Add token to SSE connection URLs
const logsURL = token
    ? `/ui/logs/${runID}?token=${encodeURIComponent(token)}`
    : `/ui/logs/${runID}`;
const logsEventSource = new EventSource(logsURL);

const statusURL = token
    ? `/ui/status-stream?token=${encodeURIComponent(token)}`
    : `/ui/status-stream`;
const statusEventSource = new EventSource(statusURL);
```

**File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/server/templates/pages/index.html`**

#### 6.2 Ensure run links don't include tokens
- Admin accesses index via Basic Auth (no tokens needed)
- Links remain as `/ui/run/<id>` without query parameters

### 7. Agent Initialization

**File: `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/cmd/mise-ci/agent.go`**

#### 7.1 Create AuthMiddleware instance
```go
authMiddleware := server.NewAuthMiddleware(appCore, &server.AuthConfig{
    AdminUsername: cfg.Auth.AdminUsername,
    AdminPassword: cfg.Auth.AdminPassword,
})
```

#### 7.2 Pass to HttpServer constructor
```go
httpSrv := server.NewHttpServer(
    cfg.Server.HTTPAddr,
    svc,
    wsSrv,
    uiSrv,
    authMiddleware,  // NEW
    logger,
)
```

#### 7.3 Add logging for auth status
```go
authConfigured := cfg.Auth.AdminUsername != "" && cfg.Auth.AdminPassword != ""
if !authConfigured {
    logger.Warn("admin credentials not configured - /ui/ endpoint will be unprotected")
    logger.Warn("set MISE_CI_AUTH_ADMIN_USERNAME and MISE_CI_AUTH_ADMIN_PASSWORD")
}

logger.Info("authentication",
    "admin_auth_enabled", authConfigured,
    "ui_tokens_enabled", true)
```

## Critical Files to Modify

1. `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/core/core.go` - JWT token generation, validation, RunInfo structure
2. `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/server/auth.go` - **NEW FILE** - Authentication middleware
3. `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/server/http.go` - HTTP server with auth middleware
4. `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/core/service.go` - GitHub status updates with pre-auth URLs
5. `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/config/config.go` - Configuration structure
6. `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/cmd/mise-ci/main.go` - Environment variable bindings
7. `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/cmd/mise-ci/agent.go` - Agent initialization with auth
8. `/home/lucasew/WORKSPACE/OPENSOURCE-own/mise-ci/internal/server/templates/pages/run.html` - Token propagation to SSE

## Security Considerations

### Tokens in URLs
- **Risk**: Visible in browser history and server logs
- **Mitigation**: Tokens are scoped to specific run ID only, no cross-run access
- **Note**: Acceptable for CI status viewing (similar to CircleCI, Travis CI patterns)

### Password Security
- Using `crypto/subtle.ConstantTimeCompare` to prevent timing attacks
- Passwords stored in environment variables (acceptable for internal CI)
- Always use HTTPS in production to protect credentials in transit

### Token Permanence
- UI tokens never expire (as requested)
- Only grant read access to specific run data
- Cannot modify runs or access other runs
- Cannot access admin endpoints

## Testing Checklist

1. **Basic Auth on Index**
   - [ ] `/ui/` without credentials → 401
   - [ ] `/ui/` with wrong credentials → 401
   - [ ] `/ui/` with correct credentials → 200

2. **Token-based Run Access**
   - [ ] `/ui/run/<id>` without token → 401
   - [ ] `/ui/run/<id>` with wrong token → 401/403
   - [ ] `/ui/run/<id>` with correct token → 200
   - [ ] `/ui/run/<other-id>` with token for different run → 403

3. **SSE Endpoints**
   - [ ] `/ui/logs/<id>?token=<valid>` → streams logs
   - [ ] `/ui/logs/<id>` without token → 401
   - [ ] `/ui/status-stream?token=<valid>` → streams status
   - [ ] `/ui/status-stream` with Basic Auth → streams status

4. **GitHub Integration**
   - [ ] Webhook triggers run
   - [ ] GitHub status includes pre-authenticated URL
   - [ ] Clicking URL from GitHub opens run without additional auth

## Environment Variables

```bash
# Required (existing)
MISE_CI_JWT_SECRET=your-secret-key

# Optional (new) - for protecting admin UI
MISE_CI_AUTH_ADMIN_USERNAME=admin
MISE_CI_AUTH_ADMIN_PASSWORD=your-secure-password
```

## Implementation Order

1. **Phase 1**: Core JWT updates (token types, validation, UIToken field)
2. **Phase 2**: Authentication middleware (new auth.go file)
3. **Phase 3**: Configuration (AuthConfig, env vars)
4. **Phase 4**: HTTP server integration (apply middleware to routes)
5. **Phase 5**: Service integration (pre-auth URLs in GitHub statuses)
6. **Phase 6**: UI templates (token propagation to SSE)
7. **Phase 7**: Agent initialization (wire up auth middleware)
8. **Phase 8**: Testing and validation
