# GitHub Forge Driver & Artifact Storage

## Goal
1. Implement `UploadReleaseAsset` in the GitHub forge driver
2. Remove `UploadArtifact` from forge interface - create separate artifact storage service
3. Implement artifact storage using `io.FS` for pluggability

## Background

The GitHub forge driver at `internal/forge/github/github.go` currently has two methods that return "not implemented" errors.

### Key Requirements (from user)
1. **UploadArtifact**: Remove from forge interface, create separate artifact storage service
   - Use `io.FS` interface for pluggability
   - Current implementation: save to `data/artifacts/` directory
   - Designed for future implementations (S3, etc.)
2. **UploadReleaseAsset**: Implement in GitHub forge
   - Upload to existing GitHub release by tag
   - Fail if release doesn't exist, never create releases

## Implementation Plan

### Part 1: Create Artifact Storage Interface & Implementation

#### 1.1. Define ArtifactStorage Interface

**Purpose**: Pluggable artifact storage abstraction

**New File**: `internal/artifacts/artifacts.go`

**Interface**:
```go
package artifacts

type Storage interface {
    // Save saves an artifact for a given runID
    Save(ctx context.Context, runID string, name string, data io.Reader) error

    // Get retrieves an artifact (for future use)
    Get(ctx context.Context, runID string, name string) (io.ReadCloser, error)

    // List lists all artifacts for a runID (for future use)
    List(ctx context.Context, runID string) ([]string, error)
}
```

#### 1.2. Implement LocalStorage

**Purpose**: Local filesystem implementation of ArtifactStorage

**Location**: `internal/artifacts/local.go`

**Implementation**:
```go
type LocalStorage struct {
    basePath string  // e.g., "{dataDir}/artifacts"
}

func NewLocalStorage(basePath string) *LocalStorage {
    return &LocalStorage{basePath: basePath}
}

func (ls *LocalStorage) Save(ctx context.Context, runID string, name string, data io.Reader) error {
    // 1. Construct path: {basePath}/{runID}/{name}
    // 2. Create directory if needed: {basePath}/{runID}
    // 3. Create file
    // 4. Copy data from io.Reader
    // 5. Return any errors
}
```

**Note**: Artifacts are flat files in `{basePath}/{runID}/` - no subdirectories

### Part 2: Update Forge Interface

#### 2.1. Remove UploadArtifact from Forge Interface

**Location**: `internal/forge/forge.go`

**Change**: Remove the `UploadArtifact` method from the `Forge` interface

**Rationale**: Artifacts are not forge-specific - they're a separate concern

#### 2.2. Remove UploadArtifact from GitHub Implementation

**Location**: `internal/forge/github/github.go`

**Change**: Delete the `UploadArtifact` method

### Part 3: Implement GitHub UploadReleaseAsset

**Strategy**: Upload to existing release identified by tag parameter

**Location**: `internal/forge/github/github.go`

**Implementation Flow**:
1. Parse repo into owner/name (validate format)
2. Get authenticated client:
   - Call `getAppClient()` to get JWT-authenticated client
   - Find repository installation for owner/repo
   - Create installation token
   - Create new client with installation token
3. Get existing release by tag using `client.Repositories.GetReleaseByTag()`
4. If release doesn't exist (404), return error: `"release with tag '%s' not found"`
5. Upload asset:
   - Create temp file from `io.Reader` (required by go-github)
   - Upload using `client.Repositories.UploadReleaseAsset()`
   - Clean up temp file with defer
6. Return any errors

**No Release Creation**: This method never creates releases - it only uploads to existing ones

#### Optional Helpers (Recommended)

**Helper: `getInstallationClient(ctx, repo)`**
- Encapsulates: get app client → find installation → create token → return client
- Can be reused by `UpdateStatus` to reduce duplication

**Helper: `uploadAssetToRelease(ctx, client, owner, repo, releaseID, name, data)`**
- Encapsulates: create temp file → upload → cleanup

### Part 4: Integration

#### 4.1. Update Service to Use Artifact Storage

**Location**: `internal/core/service.go`

**Changes**:
- Add `ArtifactStorage artifacts.Storage` field to `Service` struct
- Update `NewService` to accept artifact storage parameter
- Use `s.ArtifactStorage.Save()` instead of `forge.UploadArtifact()`

#### 4.2. Update Agent Initialization

**Location**: `cmd/mise-ci/agent.go`

**Changes**:
- Create artifact storage: `artifactStorage := artifacts.NewLocalStorage(filepath.Join(cfg.Storage.DataDir, "artifacts"))`
- Pass it to service constructor

### Optional: Refactor UpdateStatus

**Change**: Use `getInstallationClient` helper if created

**Benefits**: Cleaner code, consistent pattern

## Error Handling

**Artifact Storage (LocalStorage)**:
- **Directory creation failure**: Return error if can't create `{basePath}/{runID}/`
- **File creation failure**: Return error if can't create file
- **Write failure**: Return error if io.Copy fails

**UploadReleaseAsset (GitHub Forge)**:
- **Invalid repo format**: Return clear error message
- **Rate limiting**: GitHub API may return 429/403 - propagate these errors
- **Release not found**: Return error: `"release with tag '%s' not found"`
- **Asset conflicts**: If asset name already exists, GitHub API returns error - propagate to caller
- **Network errors**: Propagate any GitHub API errors

## Testing Strategy

**Artifact Storage**:
1. Save artifact - verify file created at `{basePath}/{runID}/{name}`
2. Save multiple artifacts to same runID - verify all files in same directory
3. Verify file contents match uploaded data

**UploadReleaseAsset**:
1. Upload to non-existent tag - verify error message
2. Upload to existing tag - verify asset appears in GitHub release
3. Upload duplicate asset name - verify error handling
4. Invalid repo format - verify error handling

## Files to Create

- `internal/artifacts/artifacts.go` - Interface definition
- `internal/artifacts/local.go` - Local filesystem implementation

## Files to Modify

- `internal/forge/forge.go` - Remove `UploadArtifact` from interface
- `internal/forge/github/github.go` - Remove `UploadArtifact`, implement `UploadReleaseAsset`
- `internal/core/service.go` - Add artifact storage field, update usage
- `cmd/mise-ci/agent.go` - Create and pass artifact storage

## Future Work (Not Part of This Task)
- HTTP endpoint to serve artifacts
- UI to list artifacts in run page
- Implement `Get()` and `List()` methods in artifact storage
- S3-based artifact storage implementation

## Dependencies

**Already Available**:
- `github.com/google/go-github/v66` (confirmed in go.mod) - for GitHub API
- `github.com/golang-jwt/jwt/v5` - for GitHub authentication
- Standard library: `os`, `io`, `io/fs`, `path/filepath`, `strings`, `context`

**No New Dependencies Needed**

## Integration Points

**Artifact Storage**: `internal/core/service.go` will call:
- `artifactStorage.Save()` after successful CI runs to save build artifacts locally

**Forge Interface**: `internal/core/service.go` will call:
- `forge.UploadReleaseAsset()` when processing release workflows

Currently, artifact/release uploads are not used - they'll be integrated in future work.

## Design Decisions

### Separation of Concerns
**Chosen**: Artifacts separate from forges
**Rationale**:
- Artifacts are not forge-specific (applies to all forges: GitHub, GitLab, Gitea, etc.)
- Allows pluggable implementations (local, S3, GCS, etc.)
- Cleaner architecture

### Artifact Storage Interface
**Chosen**: `io.FS`-style interface with `Save()`, `Get()`, `List()`
**Rationale**:
- Familiar pattern for Go developers
- Easy to swap implementations
- Future-proof for different storage backends

### Local Storage Path Structure
**Chosen**: `{dataDir}/artifacts/{runID}/{filename}`
**Benefits**:
- Organized by runID
- Easy to list all artifacts for a run
- Simple to serve via HTTP
- Clear separation between runs

### Artifact File Structure
**Chosen**: Flat files in `{basePath}/{runID}/{filename}` - no subdirectories
**Rationale**: Simple, straightforward storage structure

### GitHub Release Assets
**Chosen**: Upload to existing releases only, never create
**Rationale**: User explicitly requested no release creation

### Temp File for GitHub Upload
**Chosen**: Create temp file, use defer for cleanup
**Rationale**: Required by go-github API

## Implementation Order

1. Create `internal/artifacts/artifacts.go` with interface
2. Create `internal/artifacts/local.go` with LocalStorage implementation
3. Remove `UploadArtifact` from `internal/forge/forge.go`
4. Remove `UploadArtifact` from `internal/forge/github/github.go`
5. Implement `UploadReleaseAsset` in `internal/forge/github/github.go`
6. Update `internal/core/service.go` to use artifact storage
7. Update `cmd/mise-ci/agent.go` to create and wire artifact storage
8. Optionally: Extract helper methods in GitHub forge

## Success Criteria

✅ Artifact storage interface defined
✅ LocalStorage implementation working
✅ Artifacts save to `{dataDir}/artifacts/{runID}/{name}`
✅ `UploadArtifact` removed from forge interface and implementations
✅ `UploadReleaseAsset` uploads to existing GitHub releases
✅ No release creation - only uploads to existing releases
✅ Clear error messages for all failure cases
✅ Artifacts saved as flat files (no subdirectories)
✅ Service updated to use artifact storage
✅ Code follows existing patterns in the codebase
