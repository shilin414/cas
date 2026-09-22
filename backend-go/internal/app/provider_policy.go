package app

import (
	"context"
	"fmt"

	"github.com/shilin414/cas/backend-go/internal/catalog"
)

type providerPolicyReader interface {
	ProviderByKey(context.Context, string) (*catalog.Provider, error)
}

// Startup is fail-closed. An unavailable/missing policy is not permission to
// widen capacity using an environment default. Migrations seed the catalog.
func loadProviderMaxInflight(ctx context.Context, reader providerPolicyReader, key string) (int, error) {
	p, err := reader.ProviderByKey(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("read provider %s capacity policy: %w", key, err)
	}
	if p == nil || p.MaxInflight <= 0 {
		return 0, fmt.Errorf("provider %s requires a positive catalog max_inflight", key)
	}
	return p.MaxInflight, nil
}
