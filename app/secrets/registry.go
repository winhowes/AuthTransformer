package secrets

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// Plugin fetches a secret value for a given identifier.
type Plugin interface {
	Prefix() string
	Load(ctx context.Context, id string) (string, error)
}

var registry = make(map[string]Plugin)

// CacheTTL controls how long resolved secrets remain valid. A zero duration
// disables expiry so values persist until ClearCache is called.
var CacheTTL time.Duration

type cachedSecret struct {
	val    string
	expiry time.Time
}

var secretCache = struct {
	sync.RWMutex
	m map[string]cachedSecret
}{m: make(map[string]cachedSecret)}

// ClearCache empties the cached secret values.
func ClearCache() {
	secretCache.Lock()
	secretCache.m = make(map[string]cachedSecret)
	secretCache.Unlock()
}

// Register adds a secret plugin for a prefix.
func Register(p Plugin) { registry[p.Prefix()] = p }

// ValidateSecret checks that the reference uses a known prefix.
func ValidateSecret(ref string) error {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid secret reference: %s", ref)
	}
	prefix := parts[0]
	if _, ok := registry[prefix]; !ok {
		return fmt.Errorf("unknown secret source: %s", prefix)
	}
	return nil
}

// LoadSecret resolves a secret reference using the registered plugins.
func LoadSecret(ctx context.Context, ref string) (string, error) {
	secretCache.RLock()
	if c, ok := secretCache.m[ref]; ok {
		if CacheTTL <= 0 || time.Now().Before(c.expiry) {
			secretCache.RUnlock()
			return c.val, nil
		}
	}
	secretCache.RUnlock()

	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid secret reference: %s", ref)
	}
	prefix, id := parts[0], parts[1]
	p, ok := registry[prefix]
	if !ok {
		return "", fmt.Errorf("unknown secret source: %s", prefix)
	}
	val, err := p.Load(ctx, id)
	if err != nil {
		return "", err
	}
	secretCache.Lock()
	exp := time.Time{}
	if CacheTTL > 0 {
		exp = time.Now().Add(CacheTTL)
	}
	secretCache.m[ref] = cachedSecret{val: val, expiry: exp}
	secretCache.Unlock()
	return val, nil
}

// LoadRandomSecret selects one of the provided secret references at random and
// resolves it via LoadSecret. When multiple references are given a unique seed
// is used for the random generator to ensure a different selection on each
// invocation.
func LoadRandomSecret(ctx context.Context, refs []string) (string, error) {
	if len(refs) == 0 {
		return "", fmt.Errorf("no secrets provided")
	}

	var idx int
	if len(refs) == 1 {
		idx = 0
	} else {
		nBig, err := rand.Int(rand.Reader, big.NewInt(int64(len(refs))))
		if err != nil {
			return "", fmt.Errorf("rand: %w", err)
		}
		idx = int(nBig.Int64())
	}

	ref := refs[idx]
	return LoadSecret(ctx, ref)
}
