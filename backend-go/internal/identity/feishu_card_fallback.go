package identity

import (
	"context"
	"errors"
	"fmt"
)

// SendCardWithFallback retries ONCE with a legacy payload only when Feishu
// explicitly rejected the card schema/size. A network failure may have occurred
// after acceptance, so retrying that case would risk duplicate messages.
func SendCardWithFallback(ctx context.Context, content, legacy string, send func(context.Context, string) error) (bool, error) {
	err := send(ctx, content)
	if err == nil || legacy == "" || legacy == content {
		return false, err
	}
	var rejected *FeishuAPIError
	if !errors.As(err, &rejected) || (rejected.Code != 230099 && rejected.Code != 230025) {
		return false, err
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if fallbackErr := send(ctx, legacy); fallbackErr != nil {
		// Wrap ONLY the last attempt. Joining the initial definite rejection would
		// misclassify an uncertain fallback timeout as safely retryable.
		return true, fmt.Errorf("plain card fallback failed: %w", fallbackErr)
	}
	return true, nil
}
