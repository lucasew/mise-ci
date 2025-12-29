package repository

// RunFilter defines filters for listing runs
type RunFilter struct {
	RepoURL *string
	Limit   int
	Offset  int
}
