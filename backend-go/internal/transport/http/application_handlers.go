package http

import (
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
)

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bytes"
	"context"
	"database/sql"

	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	"github.com/shilin414/cas/backend-go/internal/platform/storage"
	"golang.org/x/image/webp"
)

// ───────────────────────────────────────────── application payloads ──

// applicationSummary is the DISPLAY shape (执行报告 §11, P2-1): the fields a
// consumer surface needs to render a row — no `skills`, no
// `external_resource_id`, no runtime config, no `capabilities`, unless the
// caller explicitly asks for the one application a composer is bound to
// (see workspaceBootstrap.DefaultApplication).
//
// It is a strict projection of applicationListItem so the two shapes can
// never drift: every summary is built by summaryOf(item).
type applicationSummary struct {
	ID                 int64   `json:"id"`
	Slug               string  `json:"slug"`
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	Icon               string  `json:"icon"`
	AvatarURL          string  `json:"avatar_url"`
	Color              string  `json:"color"`
	Kind               string  `json:"kind"`
	RendererKey        string  `json:"renderer_key"`
	CategorySlug       string  `json:"category_slug"`
	CategoryName       string  `json:"category_name"`
	ProviderKey        string  `json:"provider_key"`
	RuntimeType        string  `json:"runtime_type"`
	Enabled            bool    `json:"enabled"`
	IsBound            bool    `json:"is_bound"`
	IsConsumable       bool    `json:"is_consumable"`
	ConsumeBlockReason string  `json:"consume_block_reason,omitempty"`
	IsFavorite         bool    `json:"is_favorite"`
	IsDefaultAgent     bool    `json:"is_default_agent"`
	UsageCount         int64   `json:"usage_count"`
	LastUsedAt         *string `json:"last_used_at"`
	GlobalUsageCount   int64   `json:"global_usage_count"`

	// Only on default_application: the home composer renders this agent's
	// 技能 chips and gates the attachment entry on its runtime
	// capabilities, so those two fields ARE needed by the page that
	// consumes it (§11 "除非当前页面确实需要").
	Skills       []catalog.Skill `json:"skills,omitempty"`
	Capabilities map[string]any  `json:"capabilities,omitempty"`
}

func summaryOf(item applicationListItem) applicationSummary {
	return applicationSummary{
		ID: item.ID, Slug: item.Slug, Name: item.Name, Description: item.Description,
		Icon: item.Icon, AvatarURL: item.AvatarURL, Color: item.Color,
		Kind: item.Kind, RendererKey: item.RendererKey,
		CategorySlug: item.CategorySlug, CategoryName: item.CategoryName,
		ProviderKey: item.ProviderKey, RuntimeType: item.RuntimeType,
		Enabled: item.Enabled, IsBound: item.IsBound,
		IsConsumable: item.IsConsumable, ConsumeBlockReason: item.ConsumeBlockReason,
		IsFavorite: item.IsFavorite, IsDefaultAgent: item.IsDefaultAgent,
		UsageCount: item.UsageCount, LastUsedAt: item.LastUsedAt,
		GlobalUsageCount: item.GlobalUsageCount,
	}
}

// applicationListItem is the CONSUMER shape of an application (二次复审
// P2-1): everything a card / switcher row / shortcut / sheet row needs, plus
// the two runtime facts a composer needs (`capabilities`, `skills`).
//
// It deliberately does NOT carry the provider-authoring fields
// (`external_resource_id`, `identity_mode`, `execution_mode`). Those are the
// provider's own resource identity — the Aily agent id — and they belong to
// the authoring surface only: `GET /v2/applications/{id}` and the create /
// update responses (ApplicationDetail). Before this split every paged list
// and every single-row resolve leaked them to every logged-in caller.
//
// `RuntimeType` / `ProviderKey` stay because a card legitimately renders
// “飞书 Aily 自定义智能体” and because they are not secrets.
type applicationListItem struct {
	ID                 int64          `json:"id"`
	Slug               string         `json:"slug"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Icon               string         `json:"icon"`
	AvatarURL          string         `json:"avatar_url"`
	Color              string         `json:"color"`
	Kind               string         `json:"kind"`
	RendererKey        string         `json:"renderer_key"`
	ExecutorKey        string         `json:"executor_key"`
	CategorySlug       string         `json:"category_slug"`
	CategoryName       string         `json:"category_name"`
	IsPublic           bool           `json:"is_public"`
	Enabled            bool           `json:"enabled"`
	RuntimeType        string         `json:"runtime_type"`
	ProviderKey        string         `json:"provider_key"`
	Capabilities       map[string]any `json:"capabilities"`
	IsBound            bool           `json:"is_bound"`
	IsConsumable       bool           `json:"is_consumable"`
	ConsumeBlockReason string         `json:"consume_block_reason,omitempty"`
	IsFavorite         bool           `json:"is_favorite"`
	IsDefaultAgent     bool           `json:"is_default_agent"`
	CanManage          bool           `json:"can_manage"`
	UsageCount         int64          `json:"usage_count"`
	LastUsedAt         *string        `json:"last_used_at"`
	GlobalUsageCount   int64          `json:"global_usage_count"`
	// 技能配置 (agent-scoped prompt prefixes). Always present — an agent with
	// no skills sends `[]`, never `null`, so the mobile skill sheet needs no
	// null-guard and can distinguish "not loaded" from "none configured".
	Skills []catalog.Skill `json:"skills"`
}

// applicationDetail mirrors the compact authoring shape (18 keys).
type applicationDetail struct {
	ID                 int64  `json:"id"`
	Slug               string `json:"slug"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Icon               string `json:"icon"`
	AvatarURL          string `json:"avatar_url"`
	Color              string `json:"color"`
	Kind               string `json:"kind"`
	IsPublic           bool   `json:"is_public"`
	Enabled            bool   `json:"enabled"`
	CategorySlug       string `json:"category_slug"`
	CategoryName       string `json:"category_name"`
	IsDefaultAgent     bool   `json:"is_default_agent"`
	IsBound            bool   `json:"is_bound"`
	IsConsumable       bool   `json:"is_consumable"`
	ConsumeBlockReason string `json:"consume_block_reason,omitempty"`
	RuntimeType        string `json:"runtime_type"`
	ProviderKey        string `json:"provider_key"`
	ExternalResourceID string `json:"external_resource_id"`
	IdentityMode       string `json:"identity_mode"`
	ExecutionMode      string `json:"execution_mode"`
	CanManage          bool   `json:"can_manage"`
	UpdatedAt          string `json:"updated_at"`
	// 技能配置 — see applicationListItem.Skills.
	Skills []catalog.Skill `json:"skills"`
}

// avatarVersion derives the cache-busting version of an avatar from its
// storage key alone (执行报告 §9.1).
//
// The previous version was `app.UpdatedAt.Unix()`, which broke caching for
// EVERY avatar on EVERY unrelated write: `updated_at` is
// `ON UPDATE CURRENT_TIMESTAMP(3)`, and each run bumps `usage_count`, so
// chatting with an agent changed its avatar URL and re-downloaded the image
// (identical bytes) on the next render. The storage key
// `application-avatars/{appID}/{UnixNano}.{ext}` is minted once per upload and
// never changes afterwards, so a sha256 fingerprint of it changes ONLY when
// the avatar is uploaded / cleared / re-uploaded — the correct semantics.
// Chatting, usage_count bumps, favourites, renames, category edits, runtime
// changes, enable/public toggles and default-agent promotions all keep the
// same URL, letting the browser serve from cache.
func avatarVersion(avatarKey string) string {
	sum := sha256.Sum256([]byte(avatarKey))
	return hex.EncodeToString(sum[:8])
}

func avatarURL(app *catalog.Application) string {
	if app.AvatarKey == "" {
		return ""
	}
	return fmt.Sprintf("/api/v2/applications/%d/avatar?v=%s", app.ID, avatarVersion(app.AvatarKey))
}

func (s *Server) appDetail(ctx context.Context, app *catalog.Application, binding *catalog.Binding, caller *AuthenticatedUser) (applicationDetail, error) {
	manage, err := s.canMutateApplication(ctx, app, caller)
	if err != nil {
		return applicationDetail{}, err
	}
	d := applicationDetail{
		ID:             app.ID,
		Slug:           app.Slug,
		Name:           app.Name,
		Description:    app.Description,
		Icon:           app.Icon,
		AvatarURL:      avatarURL(app),
		Color:          app.Color,
		Kind:           app.Kind,
		IsPublic:       app.IsPublic,
		Enabled:        app.Enabled,
		CategorySlug:   app.CategorySlug,
		CategoryName:   app.CategoryName,
		IsDefaultAgent: app.IsDefaultAgent,
		CanManage:      manage,
		UpdatedAt:      app.UpdatedAt.UTC().Format(time.RFC3339),
		Skills:         app.Skills,
	}
	var provider *catalog.Provider
	if binding != nil && binding.Enabled {
		providers, err := s.activeProviderIndex(ctx)
		if err != nil {
			return applicationDetail{}, err
		}
		d.IsBound = true
		d.RuntimeType = binding.RuntimeType
		d.ProviderKey = binding.ProviderKey
		d.ExternalResourceID = binding.ExternalResourceID
		d.IdentityMode = binding.IdentityMode
		d.ExecutionMode = binding.ExecutionMode
		provider = providers.activeForBinding(binding)
	}
	d.IsConsumable, d.ConsumeBlockReason = consumptionStatus(app, binding, provider)
	return d, nil
}

func (s *Server) writeApplicationDetail(w http.ResponseWriter, status int, ctx context.Context,
	app *catalog.Application, binding *catalog.Binding, caller *AuthenticatedUser) {
	detail, err := s.appDetail(ctx, app, binding, caller)
	if err != nil {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, status, detail)
}

func (s *Server) hasAnyAdminPermission(ctx context.Context, caller *AuthenticatedUser, codes ...string) (bool, error) {
	if caller == nil {
		return false, nil
	}
	if caller.IsStaff {
		return true, nil
	}
	if s.AdminRBAC == nil {
		return false, nil
	}
	for _, code := range codes {
		ok, err := s.AdminRBAC.HasPermission(ctx, caller.ID, false, code)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}
func resourceReadCodes(kind string) []string {
	if kind == "chat" {
		return []string{adminrbac.PermissionResourceAgentRead, adminrbac.PermissionResourceAgentManage}
	}
	return []string{adminrbac.PermissionResourceAppRead, adminrbac.PermissionResourceAppManage}
}
func resourceManageCode(kind string) string {
	if kind == "chat" {
		return adminrbac.PermissionResourceAgentManage
	}
	return adminrbac.PermissionResourceAppManage
}
func (s *Server) canReadApplication(ctx context.Context, app *catalog.Application, caller *AuthenticatedUser) (bool, error) {
	if app == nil {
		return false, nil
	}
	return s.hasAnyAdminPermission(ctx, caller, resourceReadCodes(app.Kind)...)
}
func (s *Server) canMutateApplication(ctx context.Context, app *catalog.Application, caller *AuthenticatedUser) (bool, error) {
	if app == nil {
		return false, nil
	}
	return s.hasAnyAdminPermission(ctx, caller, resourceManageCode(app.Kind))
}
func (s *Server) canListManagementKind(ctx context.Context, kind string, caller *AuthenticatedUser) (bool, error) {
	policy, err := s.hasAnyAdminPermission(ctx, caller, adminrbac.PermissionAccessPolicyRead, adminrbac.PermissionAccessPolicyManage)
	if err != nil || policy {
		return policy, err
	}
	if kind == "chat" {
		return s.hasAnyAdminPermission(ctx, caller, adminrbac.PermissionResourceAgentRead, adminrbac.PermissionResourceAgentManage)
	}
	if kind != "all" {
		return s.hasAnyAdminPermission(ctx, caller, adminrbac.PermissionResourceAppRead, adminrbac.PermissionResourceAppManage)
	}
	agentRead, err := s.hasAnyAdminPermission(ctx, caller, adminrbac.PermissionResourceAgentRead, adminrbac.PermissionResourceAgentManage)
	if err != nil || !agentRead {
		return false, err
	}
	return s.hasAnyAdminPermission(ctx, caller, adminrbac.PermissionResourceAppRead, adminrbac.PermissionResourceAppManage)
}

// ListApplications implements the exact list semantics (scope/kind/
// include_unbound + personal usage aggregation).
func (s *Server) ListApplications(w http.ResponseWriter, r *http.Request, params genapi.ListApplicationsParams) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	ctx := r.Context()

	kind := "chat"
	if params.Kind != nil {
		k := string(*params.Kind)
		switch k {
		case "chat", "task", "custom", "all":
			kind = k
		}
	}
	scope := "public"
	if params.Scope != nil {
		sc := string(*params.Scope)
		switch sc {
		case "public", "accessible", "mine", "manage":
			scope = sc
		}
	}
	includeUnbound := params.IncludeUnbound != nil &&
		isTruthy(*params.IncludeUnbound)

	// Deprecation meter (执行报告 §16): studio surfaces use
	// /applications/page and /workspace/bootstrap now, so this series has to
	// decay to zero before the endpoint can be deleted.
	if s.Metric != nil {
		s.Metric.LegacyApplicationListRequestsTotal.Inc()
	}

	providers, err := s.activeProviderIndex(ctx)
	if err != nil {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The legacy whole-list endpoint keeps its user-wide aggregation until
	// callers migrate to the paged endpoint (which aggregates only the page
	// ids, §20).
	usage := s.personalUsage(ctx, caller.ID)
	favorites := s.personalFavorites(ctx, caller.ID)

	out := []applicationListItem{}
	seenBound := map[int64]bool{}
	listPrivileged := caller.IsStaff
	if scope == catalog.VisibleScopeManage && !listPrivileged {
		var permissionErr error
		listPrivileged, permissionErr = s.canListManagementKind(ctx, kind, caller)
		if permissionErr != nil {
			writeSimpleError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if apps, err := s.CatalogRepo.ListApplicationsWithBindings(ctx, scope, caller.ID, listPrivileged); err == nil {
		for _, item := range apps {
			if seenBound[item.App.ID] {
				continue
			}
			seenBound[item.App.ID] = true
			if kind != "all" && item.App.Kind != kind {
				continue
			}
			out = append(out, s.buildListItem(ctx, item, providers, favorites, usage, caller))
		}
	}
	// "Extras" (unbound apps) follow the reference semantics exactly:
	//   kind=chat        → only with include_unbound
	//   kind=task/custom → always
	//   kind=all         → non-chat always, chat only with include_unbound
	extras := false
	switch kind {
	case "task", "custom":
		extras = true
	case "all":
		extras = true
	default: // chat
		extras = includeUnbound
	}
	if extras {
		if apps, err := s.CatalogRepo.ListUnboundApplications(ctx, scope, kind, caller.ID, listPrivileged); err == nil {
			for _, item := range apps {
				if seenBound[item.App.ID] {
					continue
				}
				if kind == "all" && item.App.Kind == "chat" && !includeUnbound {
					continue
				}
				seenBound[item.App.ID] = true
				out = append(out, s.buildListItem(ctx, item, providers, favorites, usage, caller))
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func isTruthy(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// ────────────────────────────── keyset-paginated catalog (执行报告 §14–§17) ──

// pageUsage / pageFavorites are the per-page aggregates (§20): only the ids
// on the returned page are ever queried.
type pageUsage struct {
	Count int64
	Last  *string
}

// usageMapOf converts a repository usage aggregate into the display map
// `buildListItem` reads. Shared by the paged endpoint and the single-row
// resolve so both render `last_used_at` from the same RFC3339 projection.
func usageMapOf(usage map[int64]catalog.PersonalUsage) map[int64]pageUsage {
	out := make(map[int64]pageUsage, len(usage))
	for id, u := range usage {
		entry := pageUsage{Count: u.Count}
		if u.Last != nil {
			t := u.Last.UTC().Format(time.RFC3339)
			entry.Last = &t
		}
		out[id] = entry
	}
	return out
}

func (s *Server) ListApplicationPage(w http.ResponseWriter, r *http.Request, params genapi.ListApplicationPageParams) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	ctx := r.Context()

	kind := catalog.PageQueryKindChat
	if params.Kind != nil {
		switch k := string(*params.Kind); k {
		case catalog.PageQueryKindChat, catalog.PageQueryKindFixed, catalog.PageQueryKindAll:
			kind = k
		}
	}
	scope := "public"
	if params.Scope != nil {
		switch sc := string(*params.Scope); sc {
		case "public", "accessible", "mine", "manage":
			scope = sc
		}
	}
	// mode answers a DIFFERENT question from scope (二次复审 P0-5): scope is
	// "who may see the row" (management), mode is "may anyone actually use
	// it" (consumption). Defaulting to manage keeps the authoring pages
	// (智能体市场 / 应用中心) seeing disabled / private / unbound rows.
	mode := catalog.PageModeManage
	if params.Mode != nil {
		switch m := string(*params.Mode); m {
		case catalog.PageModeManage, catalog.PageModeConsume:
			mode = m
		}
	}
	manageAccess, permissionErr := s.canListManagementKind(ctx, kind, caller)
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if mode == catalog.PageModeManage && !manageAccess {
		writeDetail(w, http.StatusForbidden, "只有管理员可以使用管理目录。")
		return
	}
	includeUnbound := params.IncludeUnbound != nil && isTruthy(*params.IncludeUnbound)
	search := ""
	if params.Q != nil {
		search = strings.TrimSpace(*params.Q)
	}
	category := ""
	if params.CategorySlug != nil {
		category = strings.TrimSpace(*params.CategorySlug)
	}
	limit := 24
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
		if limit > 100 {
			limit = 100
		}
	}

	var cursorCreated time.Time
	var cursorID int64
	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		t, id, err := catalog.DecodePageCursor(*params.Cursor)
		if err != nil {
			writeDetail(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		cursorCreated, cursorID = t, id
	}

	// limit+1 probe: has_more without a COUNT(*) (§15).
	page, err := s.CatalogRepo.ListApplicationPage(ctx, catalog.ApplicationPageQuery{
		Scope:           scope,
		Mode:            mode,
		Kind:            kind,
		IncludeUnbound:  includeUnbound,
		Search:          search,
		CategorySlug:    category,
		Limit:           limit + 1,
		CursorCreatedAt: cursorCreated,
		CursorID:        cursorID,
		CallerID:        caller.ID,
		IsStaff:         manageAccess,
	})
	if err != nil {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(page) > limit
	if hasMore {
		page = page[:limit]
	}
	nextCursor := ""
	if hasMore && len(page) > 0 {
		last := page[len(page)-1]
		nextCursor = catalog.EncodePageCursor(last.App.CreatedAt, last.App.ID)
	}

	appIDs := make([]int64, 0, len(page))
	for _, item := range page {
		appIDs = append(appIDs, item.App.ID)
	}
	usage, err := s.CatalogRepo.UsageByApplications(ctx, caller.ID, appIDs)
	if err != nil {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	favorites, err := s.CatalogRepo.FavoritesByApplications(ctx, caller.ID, appIDs)
	if err != nil {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}

	providers, err := s.activeProviderIndex(ctx)
	if err != nil {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]applicationListItem, 0, len(page))
	for _, item := range page {
		items = append(items, s.buildListItem(ctx, item, providers, favorites, usageMapOf(usage), caller))
	}
	// Hand-rolled response (same pattern as ListApplications): the item shape
	// is the shared applicationListItem, not the generated model struct.
	writeJSON(w, http.StatusOK, struct {
		Items      []applicationListItem `json:"items"`
		NextCursor string                `json:"next_cursor"`
		HasMore    bool                  `json:"has_more"`
	}{Items: items, NextCursor: nextCursor, HasMore: hasMore})
}

// ────────────────────────────────────────────── shared list ingredients ──

// providerIndex keys providers by provider_key, the business identity shared
// with AuthorizeExecution. provider_id is only a nullable derived cache and
// must never override a conflicting provider_key.
type providerIndex struct {
	byKey map[string]*catalog.Provider
}

func newProviderIndex(providers []catalog.Provider) providerIndex {
	byKey := make(map[string]*catalog.Provider, len(providers))
	for i := range providers {
		provider := &providers[i]
		byKey[provider.Key] = provider
	}
	return providerIndex{byKey: byKey}
}

func providerIndexOf(provider *catalog.Provider) providerIndex {
	index := providerIndex{byKey: map[string]*catalog.Provider{}}
	if provider != nil && provider.Key != "" {
		index.byKey[provider.Key] = provider
	}
	return index
}

func (index providerIndex) activeForBinding(binding *catalog.Binding) *catalog.Provider {
	if binding == nil || binding.ProviderKey == "" {
		return nil
	}
	return index.byKey[binding.ProviderKey]
}

// activeProviderIndex is authoritative input to management consumability and
// capabilities. A query failure is infrastructure failure and must propagate;
// returning an empty index would misreport every chat agent as unavailable.
func (s *Server) activeProviderIndex(ctx context.Context) (providerIndex, error) {
	providers, err := s.CatalogRepo.ListActiveProviders(ctx)
	if err != nil {
		return providerIndex{}, err
	}
	return newProviderIndex(providers), nil
}

// personalUsage aggregates the CALLER's own runs per application.
//
// User-wide rather than per-page/per-pool on purpose: the row count is
// bounded by what this caller actually ran, and it is ONE query instead of an
// `IN (...)` over a whole page (the paged endpoint's variant) or millions of
// rows (a whole catalog). Best-effort by design: losing it degrades the
// 使用次数 / 最近使用 hints, it must never fail a list request.
func (s *Server) personalUsage(ctx context.Context, userID int64) map[int64]pageUsage {
	usage := map[int64]pageUsage{}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT application_id, COUNT(*) AS n, MAX(created_at) AS last_used FROM runs WHERE user_id = ? AND application_id IS NOT NULL GROUP BY application_id`,
		userID)
	if err != nil {
		return usage
	}
	defer rows.Close()
	for rows.Next() {
		var appID, n int64
		var last sql.NullTime
		if err := rows.Scan(&appID, &n, &last); err == nil {
			entry := pageUsage{Count: n}
			if last.Valid {
				t := last.Time.UTC().Format(time.RFC3339)
				entry.Last = &t
			}
			usage[appID] = entry
		}
	}
	return usage
}

// personalFavorites is the set of applications this caller starred
// (best-effort, same rationale as personalUsage).
func (s *Server) personalFavorites(ctx context.Context, userID int64) map[int64]bool {
	favorites := map[int64]bool{}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT application_id FROM application_favorites WHERE user_id = ?`, userID)
	if err != nil {
		return favorites
	}
	defer rows.Close()
	for rows.Next() {
		var appID int64
		if rows.Scan(&appID) == nil {
			favorites[appID] = true
		}
	}
	return favorites
}

func consumptionStatus(app *catalog.Application, binding *catalog.Binding, provider *catalog.Provider) (bool, string) {
	var providerStatus *string
	if provider != nil {
		providerStatus = &provider.Status
	}
	if catalog.Consumable(catalog.ConsumptionFacts{
		Application: app, Binding: binding, ProviderStatus: providerStatus,
	}) {
		return true, ""
	}
	if app == nil || !app.Enabled {
		return false, "disabled"
	}
	if app.Kind == "chat" && (binding == nil || !binding.Enabled) {
		return false, "unbound"
	}
	return false, "runtime_unavailable"
}

func (s *Server) buildListItem(ctx context.Context, item catalog.ApplicationWithBinding, providers providerIndex,
	favorites map[int64]bool, usage map[int64]pageUsage, caller *AuthenticatedUser) applicationListItem {
	app := item.App
	capsAny := map[string]any{}
	var rt, pk string
	var provider *catalog.Provider
	isBound := false
	if item.Binding != nil && item.Binding.Enabled {
		isBound = true
		rt = item.Binding.RuntimeType
		pk = item.Binding.ProviderKey
		provider = providers.activeForBinding(item.Binding)
		boolCaps := item.Binding.EffectiveCapabilities(provider)
		capsAny = map[string]any{}
		for k, v := range boolCaps {
			capsAny[k] = v
		}
	}
	isConsumable, consumeBlockReason := consumptionStatus(app, item.Binding, provider)
	entry := usage[app.ID]
	disp := ""
	if entry.Last != nil {
		disp = *entry.Last
	}
	return applicationListItem{
		ID: app.ID, Slug: app.Slug, Name: app.Name, Description: app.Description,
		Icon: app.Icon, AvatarURL: avatarURL(app), Color: app.Color,
		Kind: app.Kind, RendererKey: app.RendererKey, ExecutorKey: app.ExecutorKey,
		CategorySlug: app.CategorySlug, CategoryName: app.CategoryName,
		IsPublic:    app.IsPublic,
		Enabled:     app.Enabled,
		RuntimeType: rt, ProviderKey: pk,
		Capabilities: capsAny, IsBound: isBound,
		IsConsumable: isConsumable, ConsumeBlockReason: consumeBlockReason,
		IsFavorite:     favorites[app.ID],
		IsDefaultAgent: app.IsDefaultAgent,
		CanManage:      func() bool { ok, _ := s.canMutateApplication(ctx, app, caller); return ok }(),
		UsageCount:     entry.Count, LastUsedAt: dispTime(disp),
		GlobalUsageCount: app.UsageCount,
		Skills:           app.Skills,
	}
}

func dispTime(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GetApplication implements GET /api/v2/applications/{id} — the AUTHORING
// read (二次复审 P0-3), and a MANAGEMENT-only surface since 三次复审 P0-R4.1.
//
// The shape it answers with carries the provider-authoring fields
// (`external_resource_id`, `identity_mode`, `execution_mode`), so it is not
// a public read. `VisibleTo` used to admit every logged-in caller for a
// public application — which handed back exactly the authoring fields the
// consumer DTO split had removed from every list/resolve response. The gate
// is now `canManageCaller` (staff only); consumers read through
// /applications/resolve, /applications/page and /workspace/bootstrap.
//
// "Does not exist" and "exists but you may not manage it" both answer 404 —
// never 403 — because the id is sequential and a distinguishable 403 would
// turn this endpoint into an existence oracle for the whole catalog.
// A database failure answers 500: disguising it as 404 would make an outage
// look like "the application is gone" (三次复审 P1-R1).
func (s *Server) GetApplication(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	app, err := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeDetail(w, http.StatusNotFound, "application not found")
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canRead, permissionErr := s.canReadApplication(r.Context(), app, caller)
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !canRead {
		s.denyApplicationVisibility("detail")
		writeDetail(w, http.StatusNotFound, "application not found")
		return
	}
	binding, err := s.CatalogRepo.EnabledBinding(r.Context(), int64(id))
	if err != nil && !errors.Is(err, catalog.ErrNotFound) {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if errors.Is(err, catalog.ErrNotFound) {
		binding = nil
	}
	s.writeApplicationDetail(w, http.StatusOK, r.Context(), app, binding, caller)
}

// denyApplicationVisibility records a visibility refusal. The response is
// ALWAYS 404 (never 403), so without this signal an enumeration probe and a
// caller that merely lost access are indistinguishable in the logs.
func (s *Server) denyApplicationVisibility(surface string) {
	if s.Metric == nil || s.Metric.ApplicationVisibilityDeniedTotal == nil {
		return
	}
	s.Metric.ApplicationVisibilityDeniedTotal.WithLabelValues(surface).Inc()
}

// writeApplicationAccessError collapses the two access outcomes a
// sequential-id application surface can produce onto ONE opaque 404
// (三次复审 P0-R4.2). Before this existed the management mutations answered
// 403 for "exists but not yours" and fell through to 500 for a missing row,
// so PATCHing ids 1, 2, 3… distinguished hidden rows from absent ones — the
// by-id GET endpoints' non-disclosure was never an endpoint-level invariant.
//
//	route miss (catalog.ErrNotFound)   → 404 application not found
//	no manage right (ErrNotManageable) → 404 application not found
//	anything else (DB failure …)       → false; the caller's own mapping
//	                                     answers 500 (never a fake 404, P1-R1)
//
// It returns true when a response has been written. Applied to every
// sequential-id surface: GET detail, PATCH, DELETE, avatar up/clear,
// default-agent set/unset.
func (s *Server) writeApplicationAccessError(w http.ResponseWriter, err error, surface string) bool {
	if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrNotManageable) {
		s.denyApplicationVisibility(surface)
		writeDetail(w, http.StatusNotFound, "application not found")
		return true
	}
	return false
}

func (s *Server) CreateApplication(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	var body struct {
		Name            string          `json:"name"`
		Slug            string          `json:"slug"`
		Description     string          `json:"description"`
		Icon            string          `json:"icon"`
		Color           string          `json:"color"`
		CategorySlug    string          `json:"category_slug"`
		CategoryName    string          `json:"category_name"`
		IsPublic        *bool           `json:"is_public"`
		Kind            string          `json:"kind"`
		RendererKey     string          `json:"renderer_key"`
		Runtime         *runtimeInput   `json:"runtime"`
		SetDefaultAgent bool            `json:"set_default_agent"`
		Skills          []catalog.Skill `json:"skills"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeFieldErrors(w, map[string][]string{"body": {"invalid json"}})
		return
	}
	authorizationKind := body.Kind
	if authorizationKind == "" {
		authorizationKind = "chat"
	}
	canCreate, permissionErr := s.hasAnyAdminPermission(r.Context(), caller, resourceManageCode(authorizationKind))
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !canCreate {
		writeDetail(w, http.StatusForbidden, "没有资源管理权限。")
		return
	}
	in := &catalog.CreateInput{
		Name: body.Name, Slug: body.Slug, Description: body.Description,
		Icon: body.Icon, Color: body.Color,
		CategorySlug: body.CategorySlug, CategoryName: body.CategoryName,
		IsPublic: false,
		Kind:     body.Kind, RendererKey: body.RendererKey,
		SetDefaultAgent: body.SetDefaultAgent,
		Skills:          body.Skills,
		CreatorID:       caller.ID,
		IsStaff:         true,
	}
	if body.Runtime != nil {
		in.Runtime = body.Runtime.toCatalog()
	}
	app, binding, err := s.Catalog.Create(r.Context(), in)
	if err != nil {
		s.writeCatalogError(w, err)
		return
	}
	s.IdentityRepo.WriteAuditLog(r.Context(), &caller.ID, "application.create", strconv.FormatInt(app.ID, 10), map[string]any{"kind": app.Kind, "slug": app.Slug})
	s.writeApplicationDetail(w, http.StatusCreated, r.Context(), app, binding, caller)
}

type runtimeInput struct {
	ProviderKey        string         `json:"provider_key"`
	RuntimeType        string         `json:"runtime_type"`
	ExternalResourceID string         `json:"external_resource_id"`
	IdentityMode       string         `json:"identity_mode"`
	ExecutionMode      string         `json:"execution_mode"`
	SessionPolicy      string         `json:"session_policy"`
	ArtifactPolicy     string         `json:"artifact_policy"`
	TimeoutSeconds     int64          `json:"timeout_seconds"`
	Config             map[string]any `json:"config"`
}

func (r *runtimeInput) toCatalog() *catalog.BindingInput {
	return &catalog.BindingInput{
		ProviderKey:        r.ProviderKey,
		RuntimeType:        r.RuntimeType,
		ExternalResourceID: r.ExternalResourceID,
		IdentityMode:       r.IdentityMode,
		ExecutionMode:      r.ExecutionMode,
		SessionPolicy:      r.SessionPolicy,
		ArtifactPolicy:     r.ArtifactPolicy,
		TimeoutSeconds:     r.TimeoutSeconds,
		Config:             r.Config,
	}
}

func (s *Server) UpdateApplication(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	appForPermission, permissionErr := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if errors.Is(permissionErr, catalog.ErrNotFound) {
		writeDetail(w, http.StatusNotFound, "application not found")
		return
	}
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	canMutate, permissionErr := s.canMutateApplication(r.Context(), appForPermission, caller)
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !canMutate {
		writeDetail(w, http.StatusNotFound, "application not found")
		return
	}
	var body struct {
		Name            *string          `json:"name"`
		Description     *string          `json:"description"`
		Icon            *string          `json:"icon"`
		Color           *string          `json:"color"`
		IsPublic        *bool            `json:"is_public"`
		Enabled         *bool            `json:"enabled"`
		CategorySlug    *string          `json:"category_slug"`
		CategoryName    *string          `json:"category_name"`
		Runtime         *runtimeInput    `json:"runtime"`
		SetDefaultAgent *bool            `json:"set_default_agent"`
		Skills          *[]catalog.Skill `json:"skills"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeFieldErrors(w, map[string][]string{"body": {"invalid json"}})
		return
	}
	var runtime *catalog.BindingInput
	if body.Runtime != nil {
		runtime = body.Runtime.toCatalog()
	}
	app, binding, err := s.Catalog.Update(r.Context(), int64(id), caller.ID, true,
		body.Name, body.Description, body.Icon, body.Color, body.IsPublic, body.Enabled,
		body.CategorySlug, body.CategoryName, runtime, body.SetDefaultAgent, body.Skills)
	if err != nil {
		// Sequential-id opaque 404 first (三次复审 P0-R4.2); everything else
		// (payload validation, DB failure) keeps its own mapping.
		if s.writeApplicationAccessError(w, err, "mutation") {
			return
		}
		s.writeCatalogError(w, err)
		return
	}
	s.IdentityRepo.WriteAuditLog(r.Context(), &caller.ID, "application.update", strconv.FormatInt(app.ID, 10), map[string]any{"kind": app.Kind, "slug": app.Slug})
	s.writeApplicationDetail(w, http.StatusOK, r.Context(), app, binding, caller)
}

func (s *Server) DeleteApplication(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	appForPermission, permissionErr := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if errors.Is(permissionErr, catalog.ErrNotFound) {
		writeDetail(w, http.StatusNotFound, "application not found")
		return
	}
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	canMutate, permissionErr := s.canMutateApplication(r.Context(), appForPermission, caller)
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !canMutate {
		writeDetail(w, http.StatusNotFound, "application not found")
		return
	}
	if err := s.Catalog.Delete(r.Context(), int64(id), caller.ID, true); err != nil {
		if s.writeApplicationAccessError(w, err, "mutation") {
			return
		}
		s.writeCatalogError(w, err)
		return
	}
	s.IdentityRepo.WriteAuditLog(r.Context(), &caller.ID, "application.delete", strconv.FormatInt(int64(id), 10), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeCatalogError(w http.ResponseWriter, err error) {
	switch {
	case err == catalog.ErrNameRequired:
		writeFieldErrors(w, map[string][]string{"name": {err.Error()}})
	case err == catalog.ErrBadSlug:
		writeFieldErrors(w, map[string][]string{"slug": {err.Error()}})
	case err == catalog.ErrSlugTaken:
		writeFieldErrors(w, map[string][]string{"slug": {err.Error()}})
	case err == catalog.ErrOnlyChatRenderer || err == catalog.ErrFixedRendererRequired:
		writeFieldErrors(w, map[string][]string{"renderer_key": {err.Error()}})
	case err == catalog.ErrFixedRuntimeForbidden:
		writeFieldErrors(w, map[string][]string{"runtime": {err.Error()}})
	case err == catalog.ErrApplicationKind:
		writeFieldErrors(w, map[string][]string{"kind": {err.Error()}})
	case err == catalog.ErrNotManageable:
		writeDetail(w, http.StatusForbidden, err.Error())
	case err == catalog.ErrNotDefaultable:
		writeFieldErrors(w, map[string][]string{"detail": {err.Error()}})
	case err == catalog.ErrDeleteReferenced:
		writeDetail(w, http.StatusConflict, err.Error())
	case errors.Is(err, catalog.ErrInvalidSkill):
		// A malformed 技能配置 is the caller's payload, not a server fault:
		// surface it on the skills field (the message names the offending
		// entry) instead of a generic 500.
		writeFieldErrors(w, map[string][]string{"skills": {err.Error()}})
	case err == catalog.ErrNoBinding:
		writeBare(w, http.StatusBadRequest, err.Error())
	default:
		msg := err.Error()
		if strings.Contains(msg, "：") && (strings.Contains(msg, "提供方") || strings.Contains(msg, "运行时")) {
			// provider/runtime field errors
			field := "provider_key"
			if strings.Contains(msg, "运行时") {
				field = "runtime_type"
			}
			writeFieldErrors(w, map[string][]string{field: {msg}})
			return
		}
		if strings.Contains(msg, "不能为空") || strings.Contains(msg, "格式不正确") {
			writeFieldErrors(w, map[string][]string{"external_resource_id": {msg}})
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, msg)
	}
}

// ────────────────────────────────────────────────── favorite / default ──

// Favorite / Unfavorite (三次复审 P0-R4.3): the service now applies the
// visibility policy, so "hidden but real" and "unknown" both arrive as
// catalog.ErrNotFound — ONE opaque 404. A database failure is a 500, never a
// fake 404 (P1-R1): an outage must not look like "the application is gone".
func (s *Server) FavoriteApplication(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	if err := s.Catalog.Favorite(r.Context(), int64(id), caller.ID, caller.IsStaff); err != nil {
		if s.writeApplicationAccessError(w, err, "favorite") {
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"application_id": int64(id), "is_favorite": true})
}

func (s *Server) UnfavoriteApplication(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	if err := s.Catalog.Unfavorite(r.Context(), int64(id), caller.ID, caller.IsStaff); err != nil {
		if s.writeApplicationAccessError(w, err, "favorite") {
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"application_id": int64(id), "is_favorite": false})
}

// SetDefaultAgent: the service rejects through promoteDefaultIfEligibleTx
// (三次复审 P0-R1); access errors are opaque 404s like every by-id surface,
// and the response body's re-read no longer swallows a DB failure (P1-R1).
func (s *Server) SetDefaultAgent(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	if err := s.Catalog.SetDefaultAgent(r.Context(), int64(id), caller.ID, caller.IsStaff); err != nil {
		if s.writeApplicationAccessError(w, err, "mutation") {
			return
		}
		s.writeCatalogError(w, err)
		return
	}
	app, err := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeDetail(w, http.StatusNotFound, "application not found")
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	binding, err := s.CatalogRepo.EnabledBinding(r.Context(), int64(id))
	if err != nil && !errors.Is(err, catalog.ErrNotFound) {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if errors.Is(err, catalog.ErrNotFound) {
		binding = nil
	}
	s.writeApplicationDetail(w, http.StatusOK, r.Context(), app, binding, caller)
}

func (s *Server) UnsetDefaultAgent(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	if err := s.Catalog.UnsetDefaultAgent(r.Context(), int64(id), caller.ID, caller.IsStaff); err != nil {
		if s.writeApplicationAccessError(w, err, "mutation") {
			return
		}
		s.writeCatalogError(w, err)
		return
	}
	app, err := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeDetail(w, http.StatusNotFound, "application not found")
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	binding, err := s.CatalogRepo.EnabledBinding(r.Context(), int64(id))
	if err != nil && !errors.Is(err, catalog.ErrNotFound) {
		writeSimpleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if errors.Is(err, catalog.ErrNotFound) {
		binding = nil
	}
	s.writeApplicationDetail(w, http.StatusOK, r.Context(), app, binding, caller)
}

// ───────────────────────────────────────────────────────────── avatar ──

var allowedAvatarExts = map[string]bool{"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true}
var avatarDecodeSlots = make(chan struct{}, 2)

const (
	avatarMaxBytes          = 2 * 1024 * 1024
	avatarMultipartMaxBytes = avatarMaxBytes + 64*1024
	avatarMaxDimension      = 1024
	avatarMaxPixels         = 1024 * 1024
	avatarCleanupTimeout    = 5 * time.Second
)

// GetApplicationAvatar streams one application's avatar bytes (二次复审 P0-4).
//
// Order of checks is LOAD-BEARING:
//
//	authentication → visibility → storage existence → ETag/304
//
// The ETag short-circuit must come AFTER the visibility and storage checks. Otherwise a
// caller who lost access to an application would still get 304 Not Modified
// for an ETag their browser already cached, i.e. they would keep seeing an
// image they are no longer allowed to see (and `Cache-Control: immutable`
// would keep it for a year).
//
// This handler is no longer reachable anonymously: it was removed from
// publicRoutes, because a public by-id image made the sequential application
// id an enumeration oracle.
func (s *Server) GetApplicationAvatar(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	app, err := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if err != nil {
		// A database failure is a 500 — answering 404 would tell the caller
		// "the application is gone" during an outage (三次复审 P1-R1).
		if errors.Is(err, catalog.ErrNotFound) {
			writeDetail(w, http.StatusNotFound, "avatar not set")
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	allowed, accessErr := s.CatalogRepo.AccessAllowed(r.Context(), app.ID, caller.ID, caller.IsStaff)
	if accessErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Authoring access includes the avatar preview, not permission to execute.
	// A manager may upload a private agent's avatar before granting consumption access.
	if !allowed {
		allowed, accessErr = s.canReadApplication(r.Context(), app, caller)
		if accessErr != nil {
			writeSimpleError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if !allowed {
		s.denyApplicationVisibility("avatar")
		writeDetail(w, http.StatusNotFound, "avatar not set")
		return
	}
	if app.AvatarKey == "" {
		writeDetail(w, http.StatusNotFound, "avatar not set")
		return
	}
	rc, object, err := s.Storage.Open(r.Context(), app.AvatarKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// A stale avatar_key must not leave every Workbench surface with a
			// broken image. Keep the versioned URL stable, but render the same icon
			// fallback the frontend uses until an administrator uploads a new file.
			writeApplicationAvatarFallback(w, app.Icon)
			return
		}
		// Do not cache a temporary storage outage under the immutable avatar URL.
		w.Header().Del("ETag")
		w.Header().Set("Cache-Control", "no-store")
		writeSimpleError(w, http.StatusInternalServerError, "avatar storage unavailable")
		return
	}
	defer rc.Close()
	if object.Size <= 0 || object.Size > avatarMaxBytes {
		writeApplicationAvatarFallback(w, app.Icon)
		return
	}
	// The key fingerprints the immutable stored object. Once storage confirms
	// existence and a valid size, a matching conditional request can return
	// 304 without downloading the object body from S3/MinIO.
	version := avatarVersion(app.AvatarKey)
	etag := `"` + version + `"`
	writeAvatarCacheHeaders(w, etag)
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, readErr := io.ReadAll(io.LimitReader(rc, avatarMaxBytes+1))
	if readErr != nil || len(data) > avatarMaxBytes || int64(len(data)) != object.Size {
		writeApplicationAvatarFallback(w, app.Icon)
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(app.AvatarKey))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func writeApplicationAvatarFallback(w http.ResponseWriter, icon string) {
	// The object may be restored under the same storage key, so the fallback
	// must be retried instead of becoming the immutable representation of that
	// key. Delete any cache metadata a caller may have set before discovering
	// the missing object.
	w.Header().Del("ETag")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	value := strings.TrimSpace(icon)
	if value == "" {
		value = "AI"
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128"><rect width="128" height="128" rx="24" fill="#F1F3F5"/><text x="64" y="70" text-anchor="middle" dominant-baseline="middle" font-size="54">%s</text></svg>`, html.EscapeString(value))
}

// writeAvatarCacheHeaders centralizes the avatar cache contract.
//
// Avatar URLs are content-versioned, but authorization can change while a
// browser keeps the same session cookie. Store the private response and always
// revalidate it so the visibility check still runs after access is revoked.
func writeAvatarCacheHeaders(w http.ResponseWriter, etag string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Vary", "Cookie")
	w.Header().Set("Cache-Control", "private, no-cache")
}

func (s *Server) deleteAvatarObject(parent context.Context, key string) {
	if key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), avatarCleanupTimeout)
	defer cancel()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = s.Storage.Delete(ctx, key); err == nil {
			return
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				attempt = 3
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	if s.Log != nil {
		s.Log.Warn("delete avatar object failed", "key", key, "err", err)
	}
}

func matchesETag(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}
	return false
}

type applicationAvatarUpload struct {
	data        []byte
	ext         string
	contentType string
}

func readApplicationAvatarUpload(w http.ResponseWriter, r *http.Request) (applicationAvatarUpload, bool) {
	if r.ContentLength > avatarMultipartMaxBytes {
		writeDetail(w, http.StatusRequestEntityTooLarge, "avatar upload request is too large")
		return applicationAvatarUpload{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, avatarMultipartMaxBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeDetail(w, http.StatusRequestEntityTooLarge, "avatar upload request is too large")
			return applicationAvatarUpload{}, false
		}
		writeFieldErrors(w, map[string][]string{"file": {"请选择头像文件"}})
		return applicationAvatarUpload{}, false
	}
	defer file.Close()

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(header.Filename), "."))
	if !allowedAvatarExts[ext] {
		writeFieldErrors(w, map[string][]string{"file": {"头像仅支持 gif / jpeg / jpg / png / webp 格式"}})
		return applicationAvatarUpload{}, false
	}
	data, err := io.ReadAll(io.LimitReader(file, avatarMaxBytes+1))
	if err != nil || len(data) > avatarMaxBytes {
		writeFieldErrors(w, map[string][]string{"file": {"头像不能超过 2MB"}})
		return applicationAvatarUpload{}, false
	}
	select {
	case avatarDecodeSlots <- struct{}{}:
		defer func() { <-avatarDecodeSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		writeDetail(w, http.StatusServiceUnavailable, "avatar processing busy, retry later")
		return applicationAvatarUpload{}, false
	}
	contentType, valid := validApplicationAvatarBytes(ext, data)
	if !valid {
		writeFieldErrors(w, map[string][]string{"file": {"头像必须是完整的静态 gif / jpeg / jpg / png / webp 图片，尺寸不超过 1024×1024"}})
		return applicationAvatarUpload{}, false
	}
	return applicationAvatarUpload{data: data, ext: ext, contentType: contentType}, true
}

func validApplicationAvatarBytes(ext string, data []byte) (string, bool) {
	expectedContentTypes := map[string]string{
		"png":  "image/png",
		"jpg":  "image/jpeg",
		"jpeg": "image/jpeg",
		"gif":  "image/gif",
		"webp": "image/webp",
	}
	expected := expectedContentTypes[ext]
	if expected == "" || http.DetectContentType(data) != expected {
		return "", false
	}

	var width, height int
	var err error
	switch expected {
	case "image/png":
		if pngHasAnimation(data) {
			return "", false
		}
		config, configErr := png.DecodeConfig(bytes.NewReader(data))
		if configErr != nil {
			return "", false
		}
		width, height = config.Width, config.Height
		if !avatarDimensionsAllowed(width, height) {
			return "", false
		}
		_, err = png.Decode(bytes.NewReader(data))
	case "image/jpeg":
		config, configErr := jpeg.DecodeConfig(bytes.NewReader(data))
		if configErr != nil {
			return "", false
		}
		width, height = config.Width, config.Height
		if !avatarDimensionsAllowed(width, height) {
			return "", false
		}
		_, err = jpeg.Decode(bytes.NewReader(data))
	case "image/gif":
		if !singleFrameGIF(data) {
			return "", false
		}
		config, configErr := gif.DecodeConfig(bytes.NewReader(data))
		if configErr != nil {
			return "", false
		}
		width, height = config.Width, config.Height
		if !avatarDimensionsAllowed(width, height) {
			return "", false
		}
		_, err = gif.Decode(bytes.NewReader(data))
	case "image/webp":
		if !staticWebPContainer(data) {
			return "", false
		}
		config, configErr := webp.DecodeConfig(bytes.NewReader(data))
		if configErr != nil {
			return "", false
		}
		width, height = config.Width, config.Height
		if !avatarDimensionsAllowed(width, height) {
			return "", false
		}
		_, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return "", false
	}
	return expected, true
}

func avatarDimensionsAllowed(width, height int) bool {
	return width > 0 && height > 0 &&
		width <= avatarMaxDimension && height <= avatarMaxDimension &&
		int64(width)*int64(height) <= avatarMaxPixels
}

func pngHasAnimation(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return false
	}
	for offset := 8; offset+12 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if size < 0 || offset+12+size > len(data) {
			return false
		}
		kind := string(data[offset+4 : offset+8])
		if kind == "acTL" {
			return true
		}
		offset += 12 + size
	}
	return false
}

func singleFrameGIF(data []byte) bool {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return false
	}
	offset := 13
	if data[10]&0x80 != 0 {
		offset += 3 * (1 << ((data[10] & 0x07) + 1))
	}
	frames := 0
	for offset < len(data) {
		block := data[offset]
		offset++
		switch block {
		case 0x2c:
			frames++
			if frames > 1 || offset+9 > len(data) {
				return false
			}
			packed := data[offset+8]
			offset += 9
			if packed&0x80 != 0 {
				offset += 3 * (1 << ((packed & 0x07) + 1))
			}
			if offset >= len(data) {
				return false
			}
			offset++ // LZW minimum code size.
			var ok bool
			offset, ok = skipGIFSubBlocks(data, offset)
			if !ok {
				return false
			}
		case 0x21:
			if offset >= len(data) {
				return false
			}
			offset++ // Extension label.
			var ok bool
			offset, ok = skipGIFSubBlocks(data, offset)
			if !ok {
				return false
			}
		case 0x3b:
			return frames == 1
		default:
			return false
		}
	}
	return false
}

func skipGIFSubBlocks(data []byte, offset int) (int, bool) {
	for offset < len(data) {
		size := int(data[offset])
		offset++
		if size == 0 {
			return offset, true
		}
		if offset+size > len(data) {
			return 0, false
		}
		offset += size
	}
	return 0, false
}

func staticWebPContainer(data []byte) bool {
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	if uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return false
	}
	foundImage := false
	for offset := 12; offset+8 <= len(data); {
		kind := string(data[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		end := uint64(offset+8) + size
		if end > uint64(len(data)) {
			return false
		}
		if kind == "ANIM" || kind == "ANMF" {
			return false
		}
		if kind == "VP8 " || kind == "VP8L" {
			if foundImage {
				return false
			}
			foundImage = true
		}
		offset = int(end + size%2)
	}
	return foundImage
}

func (s *Server) UploadApplicationAvatar(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	app, err := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if err != nil {
		// Sequential-id opaque 404 for a missing row, 500 for a DB failure
		// (三次复审 P0-R4.2 / P1-R1). The same pair applies to the manage
		// check below: a 403 would confirm the row exists to a caller who
		// cannot manage it.
		if errors.Is(err, catalog.ErrNotFound) {
			writeDetail(w, http.StatusNotFound, "application not found")
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	canMutate, permissionErr := s.canMutateApplication(r.Context(), app, caller)
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !canMutate {
		s.denyApplicationVisibility("mutation")
		writeDetail(w, http.StatusNotFound, "application not found")
		return
	}
	upload, ok := readApplicationAvatarUpload(w, r)
	if !ok {
		return
	}
	key, err := storage.SanitizeKey(fmt.Sprintf("application-avatars/%d/%d%s", app.ID, time.Now().UnixNano(), "."+upload.ext))
	if err != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := s.Storage.Put(r.Context(), key, bytes.NewReader(upload.data), upload.contentType); err != nil {
		writeSimpleError(w, http.StatusInternalServerError, "avatar upload failed")
		return
	}
	old := app.AvatarKey
	result, err := s.DB.ExecContext(r.Context(), `UPDATE applications SET avatar_key = ? WHERE id = ? AND avatar_key = ?`, key, app.ID, old)
	if err != nil {
		s.deleteAvatarObject(r.Context(), key)
		writeSimpleError(w, http.StatusInternalServerError, "avatar update failed")
		return
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		s.deleteAvatarObject(r.Context(), key)
		writeDetail(w, http.StatusConflict, "avatar changed, please retry")
		return
	}
	if old != "" && old != key {
		s.deleteAvatarObject(r.Context(), old)
	}
	updated, err := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if err != nil {
		// The write succeeded; only the response read failed. A missing row
		// here means a concurrent delete, a DB failure is a 500 — never
		// re-answer a misleading success (三次复审 P1-R1).
		if errors.Is(err, catalog.ErrNotFound) {
			writeDetail(w, http.StatusNotFound, "application not found")
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	binding, err := s.CatalogRepo.EnabledBinding(r.Context(), int64(id))
	if err != nil && !errors.Is(err, catalog.ErrNotFound) {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if errors.Is(err, catalog.ErrNotFound) {
		binding = nil
	}
	s.writeApplicationDetail(w, http.StatusOK, r.Context(), updated, binding, caller)
}

func (s *Server) ClearApplicationAvatar(w http.ResponseWriter, r *http.Request, id genapi.ApplicationId) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return
	}
	app, err := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if err != nil {
		// Same opaque pair as UploadApplicationAvatar (三次复审 P0-R4.2).
		if errors.Is(err, catalog.ErrNotFound) {
			writeDetail(w, http.StatusNotFound, "application not found")
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	canMutate, permissionErr := s.canMutateApplication(r.Context(), app, caller)
	if permissionErr != nil {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !canMutate {
		s.denyApplicationVisibility("mutation")
		writeDetail(w, http.StatusNotFound, "application not found")
		return
	}
	if app.AvatarKey != "" {
		result, err := s.DB.ExecContext(r.Context(), `UPDATE applications SET avatar_key = '' WHERE id = ? AND avatar_key = ?`, app.ID, app.AvatarKey)
		if err != nil {
			writeSimpleError(w, http.StatusInternalServerError, "avatar update failed")
			return
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			writeDetail(w, http.StatusConflict, "avatar changed, please retry")
			return
		}
		s.deleteAvatarObject(r.Context(), app.AvatarKey)
	}
	updated, err := s.CatalogRepo.ApplicationByID(r.Context(), int64(id))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeDetail(w, http.StatusNotFound, "application not found")
			return
		}
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	binding, err := s.CatalogRepo.EnabledBinding(r.Context(), int64(id))
	if err != nil && !errors.Is(err, catalog.ErrNotFound) {
		writeSimpleError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if errors.Is(err, catalog.ErrNotFound) {
		binding = nil
	}
	s.writeApplicationDetail(w, http.StatusOK, r.Context(), updated, binding, caller)
}

// helpers

func paramToInt(id genapi.ApplicationId) int64 { return int64(id) }
