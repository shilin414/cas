package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shilin414/cas/backend-go/internal/businessapps"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	"github.com/shilin414/cas/backend-go/internal/feishucard"
	"github.com/shilin414/cas/backend-go/internal/identity"
)

type queryForwardTarget struct {
	ID   string `json:"id"`
	Type string `json:"target_type"`
}
type queryForwardRequest struct {
	SnapshotToken string               `json:"snapshot_token"`
	Targets       []queryForwardTarget `json:"targets"`
}
type queryForwardResult struct {
	TargetID    string `json:"target_id"`
	TargetType  string `json:"target_type"`
	OK          bool   `json:"ok"`
	AlreadySent bool   `json:"already_sent,omitempty"`
	Error       string `json:"error,omitempty"`
}

var feishuQueryTargetID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (s *Server) attachBusinessQuerySnapshot(r *http.Request, app *catalog.Application, caller *AuthenticatedUser, input businessapps.Input, result *businessapps.Result) {
	result.ForwardUnavailable = "查询成功，但快照服务暂不可用，本次结果暂不能转发"
	if s.BusinessSnapshots == nil {
		return
	}
	raw, e := json.Marshal(result.Data)
	if e != nil {
		return
	}
	snapshot, e := businessapps.NewQuerySnapshot(caller.ID, app.ID, app.RendererKey, app.Slug, app.Name, input, raw, time.Now())
	if e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if e = s.BusinessSnapshots.Put(ctx, snapshot); e != nil {
		return
	}
	result.Snapshot = snapshot.Info(caller.ID)
	result.ForwardUnavailable = ""
}
func (s *Server) getBusinessQuerySnapshot(w http.ResponseWriter, r *http.Request) {
	app, caller := s.businessAppAccess(w, r)
	if app == nil {
		return
	}
	if !businessapps.IsQuery(app.RendererKey) || s.BusinessSnapshots == nil {
		writeDetail(w, 404, businessapps.ErrSnapshotUnavailable.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	snapshot, e := s.BusinessSnapshots.Get(ctx, chi.URLParam(r, "token"))
	if e != nil || snapshot.ApplicationID != app.ID || snapshot.RendererKey != app.RendererKey || !snapshot.ExpiresAt.After(time.Now()) {
		writeDetail(w, 404, businessapps.ErrSnapshotUnavailable.Error())
		return
	}
	writeJSON(w, 200, businessapps.Result{Data: snapshot.Data, Message: "查询结果快照", Snapshot: snapshot.Info(caller.ID)})
}
func validateQueryTargets(targets []queryForwardTarget) bool {
	if len(targets) == 0 || len(targets) > feishuForwardMaxTargets {
		return false
	}
	seen := map[string]bool{}
	for _, target := range targets {
		key := target.Type + ":" + target.ID
		if (target.Type != "user" && target.Type != "chat") || !feishuQueryTargetID.MatchString(target.ID) || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}
func (s *Server) forwardBusinessQuery(w http.ResponseWriter, r *http.Request) {
	app, caller := s.businessAppAccess(w, r)
	if app == nil {
		return
	}
	if !businessapps.IsQuery(app.RendererKey) {
		writeDetail(w, 400, "仅支持转发条码与物料查询结果")
		return
	}
	var input queryForwardRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || !businessapps.ValidSnapshotToken(input.SnapshotToken) || !validateQueryTargets(input.Targets) {
		writeDetail(w, 400, "请选择1—20个不同的飞书用户或群组，并使用有效查询快照")
		return
	}
	if s.BusinessSnapshots == nil || s.BusinessForwardLimiter == nil {
		writeDetail(w, 503, "转发服务暂不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	snapshot, e := s.BusinessSnapshots.Get(ctx, input.SnapshotToken)
	if e != nil || snapshot.OwnerID != caller.ID || snapshot.ApplicationID != app.ID || snapshot.RendererKey != app.RendererKey || !snapshot.ExpiresAt.After(time.Now()) {
		writeDetail(w, 404, "查询快照不存在、已过期或不属于你，请重新查询")
		return
	}
	allowed, _, e := s.BusinessForwardLimiter.AllowKey(ctx, "business-forward:user:"+strconv.FormatInt(caller.ID, 10))
	if e != nil || !allowed {
		w.Header().Set("Retry-After", "60")
		writeDetail(w, 429, "转发过于频繁，请稍后再试")
		return
	}
	if s.Config == nil {
		writeDetail(w, 503, "完整结果地址尚未配置")
		return
	}
	link, e := businessapps.QuerySnapshotURL(s.Config.PublicBaseURL, snapshot)
	if e != nil {
		writeDetail(w, 503, "完整结果地址尚未配置")
		return
	}
	if s.FeishuAuth == nil || s.Feishu == nil {
		writeDetail(w, 503, "飞书转发服务暂不可用")
		return
	}
	uat, e := s.FeishuAuth.UserAccessToken(ctx, caller.ID)
	if e != nil {
		writeDetail(w, 400, "请先绑定飞书账号后使用转发")
		return
	}
	sender := caller.DisplayName
	if sender == "" {
		sender = caller.Username
	}
	prepared, prepareErr := feishucard.Prepare(businessapps.QueryResultCardOptions(snapshot, sender, link))
	if prepareErr != nil {
		writeDetail(w, 500, "卡片生成失败")
		return
	}
	if prepared.Downgraded && s.Log != nil {
		s.Log.Warn("query card conversion failed; using plain preview", "application_id", app.ID)
	}
	results := make([]queryForwardResult, len(input.Targets))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for index, target := range input.Targets {
		wg.Add(1)
		go func(index int, target queryForwardTarget) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				results[index] = queryForwardResult{TargetID: target.ID, TargetType: target.Type, Error: "本次未发送，请重试该目标"}
				return
			}
			results[index] = s.sendBusinessQueryTarget(ctx, snapshot, target, uat, prepared.Content, prepared.Fallback)
		}(index, target)
	}
	wg.Wait()
	success := 0
	for _, result := range results {
		if result.OK {
			success++
		}
	}
	if s.Log != nil {
		s.Log.Info("business query forwarded", "user_id", caller.ID, "application_id", app.ID, "success_count", success, "fail_count", len(results)-success)
	}
	writeJSON(w, 200, map[string]any{"results": results, "success_count": success, "fail_count": len(results) - success})
}
func (s *Server) sendBusinessQueryTarget(ctx context.Context, snapshot *businessapps.QuerySnapshot, target queryForwardTarget, uat, card string, fallbackCards ...string) queryForwardResult {
	result := queryForwardResult{TargetID: target.ID, TargetType: target.Type}
	targetKey := target.Type + ":" + target.ID
	state, lease, e := s.BusinessSnapshots.Claim(ctx, snapshot.Token, targetKey)
	if e != nil {
		result.Error = "转发状态暂不可用，本次未发送"
		return result
	}
	if state == "sent" {
		result.OK = true
		result.AlreadySent = true
		return result
	}
	if state == "expired" {
		result.Error = "查询快照已过期，请重新查询"
		return result
	}
	if state == "uncertain" {
		result.Error = "此前发送状态未确认，为避免重复消息，本快照不再向此目标重发，请先在飞书核实"
		return result
	}
	if state != "claimed" {
		result.Error = "该目标正在发送中，请稍后确认"
		return result
	}
	receiveType := "open_id"
	if target.Type == "chat" {
		receiveType = "chat_id"
	}
	sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	legacy := ""
	if len(fallbackCards) > 0 {
		legacy = fallbackCards[0]
	}
	fallback, sendErr := identity.SendCardWithFallback(sendCtx, card, legacy, func(attemptCtx context.Context, content string) error {
		return s.Feishu.SendIMMessageWithUUID(attemptCtx, uat, receiveType, target.ID, "interactive", content, businessapps.QuerySendUUID(snapshot.Token, targetKey))
	})
	e = sendErr
	if fallback && s.Log != nil {
		s.Log.Warn("query card downgraded to plain preview", "application_id", snapshot.ApplicationID, "fallback_sent", e == nil)
	}
	cancel()
	result.OK = e == nil
	outcome := "sent"
	if e != nil {
		outcome = "uncertain"
		result.Error = "发送状态未确认，为避免重复消息，本快照不再向此目标重发，请先在飞书核实"
		var upstream *identity.FeishuAPIError
		if errors.As(e, &upstream) && upstream.Code != 0 {
			outcome = "failed"
			result.Error = "飞书未接受此消息，请稍后重试该目标"
			if upstream.Code == 99991679 {
				result.Error = "飞书权限不足，需要重新授权"
			}
			if upstream.Code == 99991668 {
				result.Error = "飞书授权已过期，请重新绑定飞书账号"
			}
		}
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finishCancel()
	if finishErr := s.BusinessSnapshots.Finish(finishCtx, snapshot.Token, targetKey, lease, outcome); finishErr != nil && s.Log != nil {
		s.Log.Error("business query delivery state unavailable", "application_id", snapshot.ApplicationID)
	}
	return result
}
