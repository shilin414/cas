// Package rolemonitor provides private, read-only process observability. It never
// claims runs, triggers schedules or performs recovery on behalf of a probe.
package rolemonitor

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address                                     string
	AllowRemote                                 bool
	ProbeTimeout, SampleTimeout, RequestTimeout time.Duration
	SampleInterval, StaleAfter                  time.Duration
}

// ParseEnv uses a role-specific prefix (WORKER or SCHEDULER). Empty ADDR disables
// both listener and background sampling. Only IP literals are accepted, avoiding
// DNS changes turning a loopback listener into a public listener.
func ParseEnv(role string, getenv func(string) string) (Config, error) {
	c := Config{ProbeTimeout: time.Second, SampleTimeout: 2 * time.Second, RequestTimeout: 3 * time.Second, SampleInterval: 15 * time.Second, StaleAfter: 2 * time.Minute}
	if role != "WORKER" && role != "SCHEDULER" {
		return c, fmt.Errorf("unsupported monitor role")
	}
	prefix := role + "_MONITOR_"
	c.Address = strings.TrimSpace(getenv(prefix + "ADDR"))
	if raw := getenv(prefix + "ALLOW_REMOTE"); raw != "" {
		if raw != "true" && raw != "false" {
			return c, fmt.Errorf("%sALLOW_REMOTE must be true or false", prefix)
		}
		c.AllowRemote = raw == "true"
	}
	for _, v := range []struct {
		name string
		dst  *time.Duration
	}{{"PROBE_TIMEOUT", &c.ProbeTimeout}, {"SAMPLE_TIMEOUT", &c.SampleTimeout}, {"REQUEST_TIMEOUT", &c.RequestTimeout}, {"SAMPLE_INTERVAL", &c.SampleInterval}, {"STALE_AFTER", &c.StaleAfter}} {
		if raw := getenv(prefix + v.name); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return c, fmt.Errorf("invalid %s%s duration", prefix, v.name)
			}
			*v.dst = d
		}
	}
	return c, c.validate()
}
func (c Config) validate() error {
	for _, d := range []time.Duration{c.ProbeTimeout, c.SampleTimeout, c.RequestTimeout} {
		if d <= 0 || d > 5*time.Second {
			return fmt.Errorf("monitor timeouts must be in (0,5s]")
		}
	}
	if c.RequestTimeout <= c.ProbeTimeout {
		return fmt.Errorf("monitor request timeout must exceed probe timeout")
	}
	if c.SampleInterval < minSampleInterval || c.SampleInterval < 2*c.SampleTimeout || c.SampleInterval > 5*time.Minute {
		return fmt.Errorf("monitor sample interval must be >=5s, >=2 sample timeouts and <=5m")
	}
	if c.StaleAfter < 2*c.SampleInterval || c.StaleAfter > 30*time.Minute {
		return fmt.Errorf("monitor stale window must be >=2 sample intervals and <=30m")
	}
	if c.Address == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("monitor address must be IP:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("monitor port must be 1..65535")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("monitor address requires an explicit IP literal")
	}
	if !ip.IsLoopback() && !c.AllowRemote {
		return fmt.Errorf("non-loopback monitor listener requires explicit ALLOW_REMOTE=true and network isolation")
	}
	return nil
}

// LoopBudget describes the real loop period and its maximum sequential work
// budget. A zero OperationBudget means no finite enforced pass deadline (e.g.
// scheduler), not a promise that work is instantaneous. Such work can legitimately
// outlast the health window and become NotReady.
type LoopBudget struct {
	Interval        time.Duration
	OperationBudget time.Duration
}

// ValidateLoopBudgets requires room for two periods plus two work budgets. Role
// owners return these values from their actual constants/normalization helpers;
// command entrypoints must not copy the delivery reclaim interval as a literal.
func (c Config) ValidateLoopBudgets(loops map[string]LoopBudget) error {
	if c.Address == "" {
		return nil
	}
	for name, b := range loops {
		if b.Interval <= 0 || b.OperationBudget < 0 || b.Interval >= c.StaleAfter || b.OperationBudget >= c.StaleAfter || c.StaleAfter <= 2*(b.Interval+b.OperationBudget) {
			return fmt.Errorf("monitor stale window must exceed 2*(interval+operation budget) for loop %s", name)
		}
	}
	return nil
}
