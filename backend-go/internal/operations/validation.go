package operations

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var decimalIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var runStatuses = map[string]bool{"active": true, "all": true, "queued": true, "running": true, "waiting_input": true, "waiting_external": true, "cancelling": true, "cancelled": true, "succeeded": true, "failed": true, "interrupted": true}

func (in CapacityInput) Validate() error {
	if in.MaxInflight < 1 || in.MaxInflight > 10000 {
		return fmt.Errorf("%w: max_inflight must be 1..10000", ErrInvalid)
	}
	if in.ExpectedMaxInflight < 0 || in.ExpectedMaxInflight > 4294967295 {
		return fmt.Errorf("%w: invalid expected_max_inflight", ErrInvalid)
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > 500 || strings.ContainsRune(reason, 0) {
		return fmt.Errorf("%w: reason must be 1..500 characters", ErrInvalid)
	}
	return nil
}
func normalizeFilter(f RunFilter) (RunFilter, error) {
	if f.Status == "" {
		f.Status = "active"
	}
	if !runStatuses[f.Status] {
		return f, fmt.Errorf("%w: invalid status", ErrInvalid)
	}
	if f.Provider != "" && !providerPattern.MatchString(f.Provider) {
		return f, fmt.Errorf("%w: invalid provider", ErrInvalid)
	}
	for _, id := range []string{f.OwnerUserID, f.ApplicationID} {
		if id != "" {
			if !decimalIDPattern.MatchString(id) {
				return f, fmt.Errorf("%w: invalid numeric identifier", ErrInvalid)
			}
			if _, err := strconv.ParseInt(id, 10, 64); err != nil {
				return f, fmt.Errorf("%w: identifier out of range", ErrInvalid)
			}
		}
	}
	if f.Limit == 0 {
		f.Limit = 50
	}
	if f.Limit < 1 || f.Limit > 100 {
		return f, fmt.Errorf("%w: limit must be 1..100", ErrInvalid)
	}
	if f.Cursor != "" {
		if _, err := decodeCursor(f.Cursor, f); err != nil {
			return f, err
		}
	}
	return f, nil
}

type cursorWire struct {
	CreatedAt string `json:"at"`
	ID        string `json:"id"`
	Scope     string `json:"scope"`
}
type runCursor struct {
	CreatedAt time.Time
	ID        ids.ID
}

func filterScope(f RunFilter) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{f.Status, f.Provider, f.OwnerUserID, f.ApplicationID}, "|")))
	return hex.EncodeToString(sum[:])
}
func encodeCursor(at time.Time, id string, f RunFilter) string {
	raw, _ := json.Marshal(cursorWire{at.UTC().Format(time.RFC3339Nano), id, filterScope(f)})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeCursor(token string, f RunFilter) (runCursor, error) {
	bad := fmt.Errorf("%w: invalid or mismatched cursor", ErrInvalid)
	if len(token) > 512 {
		return runCursor{}, bad
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return runCursor{}, bad
	}
	var wire cursorWire
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return runCursor{}, bad
	}
	if dec.Decode(new(any)) != io.EOF {
		return runCursor{}, bad
	}
	if wire.Scope != filterScope(f) {
		return runCursor{}, bad
	}
	at, err := time.Parse(time.RFC3339Nano, wire.CreatedAt)
	if err != nil || at.IsZero() || wire.CreatedAt != at.UTC().Format(time.RFC3339Nano) {
		return runCursor{}, bad
	}
	id, err := ids.Parse(wire.ID)
	if err != nil || id.IsZero() || id.String() != wire.ID {
		return runCursor{}, bad
	}
	return runCursor{at, id}, nil
}
