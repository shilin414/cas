package app

import (
	"context"
	"errors"
	"testing"

	"github.com/shilin414/cas/backend-go/internal/catalog"
)

type policyReaderStub struct {
	value *catalog.Provider
	err   error
}

func (s policyReaderStub) ProviderByKey(context.Context, string) (*catalog.Provider, error) {
	return s.value, s.err
}
func TestProviderPolicyFailsClosed(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value *catalog.Provider
		err   error
		want  int
	}{
		{"valid", &catalog.Provider{MaxInflight: 20}, nil, 20},
		{"database unavailable", nil, errors.New("db down"), 0},
		{"missing", nil, nil, 0},
		{"zero is not unlimited", &catalog.Provider{}, nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n, err := loadProviderMaxInflight(context.Background(), policyReaderStub{tt.value, tt.err}, "feishu_aily")
			if tt.want > 0 {
				if err != nil || n != tt.want {
					t.Fatalf("n=%d err=%v", n, err)
				}
			} else if err == nil {
				t.Fatalf("policy error widened capacity to %d", n)
			}
		})
	}
}
