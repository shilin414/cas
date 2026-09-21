package integration

import (
	"os"
	"strings"
	"testing"
)

func TestAIModelPresetMigrationIsSecretFreeAndPortable(t *testing.T) {
	raw, err := os.ReadFile("../../db/migrations/0044_ai_model_presets.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{"glm-4.6v-flashx", "http://192.168.212.121:3000/v1", "timeout_seconds,max_concurrency", "600,2", "PP-OCRv6_tiny", "ocr-assets/ppocrv6-tiny-20260921/PP-OCRv6_tiny_det.tar"} {
		if !strings.Contains(sql, required) {
			t.Errorf("preset migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(sql), "sk-") || strings.Contains(sql, "Bearer ") {
		t.Fatal("preset migration must never contain an API credential")
	}
}
