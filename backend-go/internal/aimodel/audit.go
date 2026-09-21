package aimodel

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/dbtypes"
)

type actorContextKey struct{}

// WithActor attaches a verified management caller, not an ID taken from a request
// body. HTTP configuration mutations must use it. Runtime APIs are unaffected.
// Internal callers may omit it to explicitly operate without a management audit.
func WithActor(ctx context.Context, actorID int64) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actorID)
}

// Only field names, versions and enablement bits are accepted here. In particular,
// never pass configuration structs, endpoint strings, credentials or manifests.
type auditDetail struct {
	ChangedFields []string `json:"changed_fields"`
	BeforeVersion int64    `json:"before_version,omitempty"`
	AfterVersion  int64    `json:"after_version,omitempty"`
	EnabledBefore *bool    `json:"enabled_before,omitempty"`
	EnabledAfter  *bool    `json:"enabled_after,omitempty"`
}

func writeAdminAudit(ctx context.Context, q *db.Queries, action, resource, id string, detail auditDetail) error {
	actor, present := ctx.Value(actorContextKey{}).(int64)
	if !present {
		return nil
	}
	if actor <= 0 {
		return invalid("audit actor")
	}
	if detail.ChangedFields == nil {
		detail.ChangedFields = []string{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return ErrUnavailable
	}
	return q.CreateAIAdminAudit(ctx, db.CreateAIAdminAuditParams{UserID: sql.NullInt64{Int64: actor, Valid: true}, Action: action, Resource: resource, ResourceID: id, Detail: dbtypes.JSONText(raw)})
}
func connectionChangedFields(before *Connection, after ConnectionInput) []string {
	fields := []string{}
	add := func(changed bool, name string) {
		if changed {
			fields = append(fields, name)
		}
	}
	add(before.Name != after.Name, "name")
	add(before.Adapter != after.Adapter, "adapter")
	add(before.BaseURL != after.BaseURL, "base_url")
	add(before.Enabled != after.Enabled, "enabled")
	add(before.TimeoutSeconds != after.TimeoutSeconds, "timeout_seconds")
	add(before.MaxConcurrency != after.MaxConcurrency, "max_concurrency")
	return fields
}
func sameAuditJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(manifestJSON(a), &x) != nil || json.Unmarshal(manifestJSON(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
func modelChangedFields(before *Model, after ModelInput) []string {
	fields := []string{}
	add := func(changed bool, name string) {
		if changed {
			fields = append(fields, name)
		}
	}
	add(before.Name != after.Name, "name")
	add(before.ModelID != after.ModelID, "model_id")
	add(before.CapabilityKind != after.CapabilityKind, "capability_kind")
	add(before.ExecutionLocation != after.ExecutionLocation, "execution_location")
	add(!reflect.DeepEqual(before.ConnectionID, after.ConnectionID), "connection_id")
	add(!sameAuditJSON(before.BrowserManifest, after.BrowserManifest), "browser_manifest")
	add(before.Capabilities != after.Capabilities, "capabilities")
	add(!reflect.DeepEqual(before.DefaultParameters, after.DefaultParameters), "default_parameters")
	add(before.Enabled != after.Enabled, "enabled")
	return fields
}
