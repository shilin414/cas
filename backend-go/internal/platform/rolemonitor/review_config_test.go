package rolemonitor

import (
	"testing"
	"time"
)

func TestProductionSamplingRateCannotBecomeTightLoop(t *testing.T) {
	for _, tt := range []struct {
		interval, timeout string
		valid             bool
	}{
		{"100ms", "100ms", false}, {"2s", "100ms", false}, {"4999ms", "100ms", false},
		{"5s", "100ms", true}, {"5s", "3s", false}, {"6s", "3s", true},
	} {
		t.Run(tt.interval+"_"+tt.timeout, func(t *testing.T) {
			_, err := ParseEnv("WORKER", func(k string) string {
				switch k {
				case "WORKER_MONITOR_SAMPLE_INTERVAL":
					return tt.interval
				case "WORKER_MONITOR_SAMPLE_TIMEOUT":
					return tt.timeout
				}
				return ""
			})
			if (err == nil) != tt.valid {
				t.Fatalf("interval=%s timeout=%s err=%v", tt.interval, tt.timeout, err)
			}
		})
	}
}
func TestSamplingRateValidationAlsoAppliesToProgrammaticConfig(t *testing.T) {
	c := configForTest()
	c.SampleInterval = 100 * time.Millisecond
	c.SampleTimeout = 100 * time.Millisecond
	if err := c.validate(); err == nil {
		t.Fatal("programmatic config bypassed production minimum")
	}
}
