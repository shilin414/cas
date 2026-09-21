package schedule

import (
	"fmt"
	"strings"
)

// DeliveryCondition is a case-sensitive literal predicate on FINAL assistant
// output.text only. Missing conditions retain the historical always behavior.
type DeliveryCondition struct {
	Operator string `json:"operator"`
	Text     string `json:"text,omitempty"`
}

func NormalizeCondition(c *DeliveryCondition) DeliveryCondition {
	if c == nil {
		return DeliveryCondition{Operator: "always"}
	}
	out := *c
	if out.Operator == "" {
		out.Operator = "always"
	}
	return out
}
func (c DeliveryCondition) Validate() error {
	switch c.Operator {
	case "always":
		return nil
	case "contains", "not_contains":
		if strings.TrimSpace(c.Text) == "" {
			return fmt.Errorf("delivery condition text is required for %s", c.Operator)
		}
		return nil
	default:
		return fmt.Errorf("unsupported delivery condition operator %q", c.Operator)
	}
}
func (c DeliveryCondition) Matches(finalReply string) bool {
	c = NormalizeCondition(&c) // zero-value historical snapshots are unconditional
	if c.Validate() != nil {
		return false
	}
	switch c.Operator {
	case "", "always":
		return true
	case "contains":
		return strings.Contains(finalReply, c.Text)
	case "not_contains":
		return !strings.Contains(finalReply, c.Text)
	default:
		return false
	}
}
