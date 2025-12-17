# Plan: Complete Authentication Implementation for mise-ci

## Overview

The staged diff implements a two-tier authentication system:
- **Admin basic auth** for listing all jobs (`/ui/`)
- **UI tokens** for viewing specific job results with shareable links

This plan completes the implementation by fixing 3 identified gaps.

## User Requirements

✅ **GitHub status links**: Include UI token automatically
✅ **Token lifetime**: Keep permanent tokens (no expiration)
✅ **Test dispatch**: Return UI URL in JSON response
✅ **Status stream**: Always require authentication (no unauthenticated access)

## Implementation Tasks

### 1. Add TargetURL to GitHub Status Updates

**File:** `internal/core/service.go`
**Method:** `StartRun()` (lines 183-240)

**Problem:** Three `UpdateStatus()` calls have empty `TargetURL` field. GitHub status checks won't have clickable links to view logs.

**Solution:** Use existing `GetRunUIURL()` helper to generate URL with UI token.

**Critical fix:** Must call `CreateRun()` BEFORE first status update (currently it's after). The UI token is generated in `CreateRun()` and needed for the URL.

**Changes:**
1. Move line 199 (`run := s.Core.CreateRun(runID)`) to before line 189
2. After `CreateRun()`, generate public URL:
   ```go
   publicURL := s.Config.Server.PublicURL
   if publicURL == "" {
       publicURL = s.Config.Server.HTTPAddr
   }
   targetURL := s.Core.GetRunUIURL(runID, publicURL)
   ```
3. Set `TargetURL: targetURL` in all three status updates (lines 193, 221, 237)

**Result:** GitHub status checks will show link like `https://ci.example.com/ui/run/abc123-1234?token=eyJ...`

---

### 2. Update Test Dispatch Response

**File:** `internal/core/service.go`
**Method:** `HandleTestDispatch()` (lines 62-106)

**Problem:** Line 105 returns plain text. Hard to extract UI URL for automated testing.

**Solution:** Return JSON response with run metadata including UI URL.

**Changes:**
1. Add import: `"encoding/json"`
2. Replace line 105 with:
   ```go
   // Generate UI URL
   uiURL := s.Core.GetRunUIURL(runID, publicURL)

   // Return JSON response with run details
   response := map[string]string{
       "run_id": runID,
       "job_id": jobID,
       "ui_url": uiURL,
       "status": "dispatched",
   }

   w.Header().Set("Content-Type", "application/json")
   w.WriteHeader(http.StatusOK)
   json.NewEncoder(w).Encode(response)
   ```

**Result:** Test endpoint returns JSON like:
```json
{
  "run_id": "abc123-1234",
  "job_id": "nomad-job-456",
  "ui_url": "http://localhost:8080/ui/run/abc123-1234?token=eyJ...",
  "status": "dispatched"
}
```

---

### 3. Fix Status Stream Auth to Always Require Authentication

**File:** `internal/server/auth.go`
**Method:** `RequireStatusStreamAuth()` (lines 104-136)

**Problem:** Lines 120-122 allow unauthenticated access when admin credentials not configured. User wants ALWAYS require authentication.

**Solution:** Remove graceful degradation. If no credentials configured AND no valid token, return 401.

**Changes:**

Replace lines 118-123:
```go
// 2. Fallback to Basic Auth
// If no credentials configured, allow access
if m.authConfig.AdminUsername == "" || m.authConfig.AdminPassword == "" {
    next(w, r)
    return
}
```

With:
```go
// 2. Fallback to Basic Auth (always required)
// Deny access if credentials not configured
if m.authConfig.AdminUsername == "" || m.authConfig.AdminPassword == "" {
    w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
    http.Error(w, "Authentication required but not configured", http.StatusUnauthorized)
    return
}
```

**Optional but recommended:** Apply same fix to `RequireBasicAuth()` method (lines 82-100) for consistency.

---

## Additional Recommendations

### Startup Validation

**File:** `cmd/mise-ci/agent.go` (after line 127)

Add validation to catch misconfigurations early:

```go
// Validate JWT secret is set
if cfg.JWT.Secret == "" {
    return fmt.Errorf("JWT secret must be configured (MISE_CI_JWT_SECRET)")
}

// Warn if PublicURL not set
if cfg.Server.PublicURL == "" {
    logger.Warn("public URL not configured - GitHub status links may not work",
        "fallback", cfg.Server.HTTPAddr,
        "env", "MISE_CI_SERVER_PUBLIC_URL")
}
```

### Testing Checklist

After implementation:

1. ✓ Push to GitHub → verify status check has clickable link
2. ✓ `curl -X POST http://localhost:8080/test/dispatch` → verify JSON response with `ui_url`
3. ✓ Access `/ui/status-stream` without auth → expect 401
4. ✓ Access `/ui/status-stream?token=<valid>` → expect success
5. ✓ Access `/ui/status-stream` with basic auth → expect success

---

## Critical Files

- `internal/core/service.go` - Status updates (lines 183-240), test dispatch (lines 62-106)
- `internal/server/auth.go` - Status stream auth fix (lines 104-136)
- `internal/core/core.go` - Reference only (has `GetRunUIURL()` helper)
- `cmd/mise-ci/agent.go` - Optional startup validation

## Summary

**3 gaps to fix:**
1. Add `TargetURL` to GitHub status updates (move CreateRun, generate URL, set in 3 places)
2. Change test dispatch to return JSON with `ui_url` field
3. Remove graceful degradation from `RequireStatusStreamAuth` (always require auth)

**Result:** Complete authentication system with shareable job links via UI tokens and admin access via password.
