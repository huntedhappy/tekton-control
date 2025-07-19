// File: pkg/git/resolver.go
package git

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	corev1 "k8s.io/api/core/v1"
)

const (
	GitSHACacheTTLKey            = "GIT_SHA_CACHE_TTL_SECONDS"
	DefaultGitSHACacheTTLSeconds = 60 // 1 minute
	UsernameField                = "username"
	PasswordField                = "password"
)

// SHACacheEntry stores cached SHA with a timestamp.
type SHACacheEntry struct {
	SHA       string
	Timestamp time.Time
}

// GitInfo stores Git repository metadata (Revision, Branch, etc.)
type GitInfo struct {
	Revision string
	Branch   string
	URL      string
	RepoPath string
	Name     string
}

// Resolver handles Git SHA resolution with caching.
type Resolver struct {
	SHACache    map[string]SHACacheEntry
	SHAMutex    sync.RWMutex
	SHACacheTTL time.Duration
}

// NewResolver creates a new Git Resolver with SHA caching.
func NewResolver() *Resolver {
	ttl := time.Duration(DefaultGitSHACacheTTLSeconds) * time.Second
	if v := os.Getenv(GitSHACacheTTLKey); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			ttl = time.Duration(secs) * time.Second
		}
	}
	return &Resolver{
		SHACache:    make(map[string]SHACacheEntry),
		SHACacheTTL: ttl,
	}
}

// GetGitAuthFromSecret reads Git auth info from a Secret.
func GetGitAuthFromSecret(secret *corev1.Secret) (*http.BasicAuth, error) {
	user := string(secret.Data[UsernameField])
	pass := string(secret.Data[PasswordField])
	if user == "" || pass == "" {
		return nil, fmt.Errorf("username or password not found in secret")
	}
	return &http.BasicAuth{Username: user, Password: pass}, nil
}

// ResolveGitSHA resolves the latest commit SHA for the specified branch using caching.
func (r *Resolver) ResolveGitSHA(ctx context.Context, repoURL, branch string, auth *http.BasicAuth) (string, error) {
	cacheKey := fmt.Sprintf("%s|%s", repoURL, branch)
	now := time.Now()

	r.SHAMutex.RLock()
	entry, cached := r.SHACache[cacheKey]
	r.SHAMutex.RUnlock()

	if cached && now.Sub(entry.Timestamp) < r.SHACacheTTL {
		return entry.SHA, nil
	}

	// Fetch from remote repository
	storer := memory.NewStorage()
	repo, err := git.Init(storer, nil)
	if err != nil {
		return "", fmt.Errorf("failed to init git repo: %w", err)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{repoURL}})
	if err != nil && err != git.ErrRemoteExists {
		return "", fmt.Errorf("failed to add remote: %w", err)
	}

	if err := repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		Depth:      1,
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", branch, branch))},
		Auth:       auth,
		Tags:       git.NoTags,
	}); err != nil && err != git.NoErrAlreadyUpToDate {
		return "", fmt.Errorf("failed to fetch branch %s: %w", branch, err)
	}

	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return "", fmt.Errorf("failed to get branch ref: %w", err)
	}

	sha := ref.Hash().String()

	r.SHAMutex.Lock()
	r.SHACache[cacheKey] = SHACacheEntry{SHA: sha, Timestamp: now}
	r.SHAMutex.Unlock()

	return sha, nil
}
