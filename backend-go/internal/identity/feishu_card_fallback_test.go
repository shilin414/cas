package identity

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestCardFallbackOnlyAfterDefinitePayloadRejection(t *testing.T) {
	transportErr := errors.New("connection reset after write")
	cases := []struct {
		name  string
		first error
		calls int
	}{
		{"success", nil, 1},
		{"card schema rejected", &FeishuAPIError{Code: 230099, Msg: "table rows invalid"}, 2},
		{"oversized card", fmt.Errorf("wrapped: %w", &FeishuAPIError{Code: 230025}), 2},
		{"auth", &FeishuAPIError{Code: 99991668}, 1},
		{"permission", &FeishuAPIError{Code: 230027}, 1},
		{"rate limit", &FeishuAPIError{Code: 230020}, 1},
		{"network uncertain", transportErr, 1},
		{"timeout", context.DeadlineExceeded, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var contents []string
			fallback, err := SendCardWithFallback(context.Background(), "native", "legacy", func(_ context.Context, content string) error {
				contents = append(contents, content)
				if len(contents) == 1 {
					return tc.first
				}
				return nil
			})
			if len(contents) != tc.calls || fallback != (tc.calls == 2) {
				t.Fatalf("unexpected fallback: %v %v", contents, fallback)
			}
			if tc.calls == 2 {
				if contents[1] != "legacy" || err != nil {
					t.Fatalf("fallback failed: %v %v", contents, err)
				}
			} else if err != tc.first {
				t.Fatalf("original failure lost: %v", err)
			}
		})
	}
}

func TestCardFallbackPreservesUncertainSecondSend(t *testing.T) {
	calls := 0
	uncertain := errors.New("fallback response lost")
	used, err := SendCardWithFallback(context.Background(), "native", "legacy", func(context.Context, string) error {
		calls++
		if calls == 1 {
			return &FeishuAPIError{Code: 230099}
		}
		return uncertain
	})
	var rejected *FeishuAPIError
	if !used || calls != 2 || !errors.Is(err, uncertain) || errors.As(err, &rejected) {
		t.Fatalf("must classify final attempt, not first rejection: calls=%d err=%v", calls, err)
	}
}

func TestCardFallbackDoesNotResendWithoutAlternateOrAfterCancellation(t *testing.T) {
	for _, legacy := range []string{"", "native"} {
		calls := 0
		_, _ = SendCardWithFallback(context.Background(), "native", legacy, func(context.Context, string) error { calls++; return &FeishuAPIError{Code: 230099} })
		if calls != 1 {
			t.Fatal("same card resent")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := SendCardWithFallback(ctx, "native", "legacy", func(context.Context, string) error { calls++; cancel(); return &FeishuAPIError{Code: 230099} })
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("sent after cancellation: %d %v", calls, err)
	}
}
