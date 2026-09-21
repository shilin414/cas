package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	"github.com/shilin414/cas/backend-go/internal/directory"
	"github.com/go-chi/chi/v5"
)

type targetConfigInput struct {
	Enabled         bool   `json:"enabled"`
	ScheduleType    string `json:"schedule_type"`
	IntervalMinutes int    `json:"interval_minutes"`
	DailyTime       string `json:"daily_time"`
	Timezone        string `json:"timezone"`
}
type batchInput struct {
	Targets []string `json:"targets"`
}

func (s *Server) registerSyncTargetRoutes(r chi.Router) {
	r.Get("/api/v2/admin/sync-targets", s.listSyncTargets)
	r.Get("/api/v2/admin/sync-targets/{target}/config", s.getSyncTargetConfig)
	r.Put("/api/v2/admin/sync-targets/{target}/config", s.updateSyncTargetConfig)
	r.Post("/api/v2/admin/sync-targets/{target}/jobs", s.createSyncTargetJob)
	r.Post("/api/v2/admin/sync-batches", s.createSyncBatch)
	r.Get("/api/v2/admin/sync-jobs", s.listSyncJobs)
	r.Get("/api/v2/admin/sync-batches/{id}", s.getSyncBatch)
}
func (s *Server) listSyncTargets(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncRead) == nil {
		return
	}
	v, err := s.DirectoryRepo.ListTargetViews(r.Context())
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) getSyncTargetConfig(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncRead) == nil {
		return
	}
	v, err := s.DirectoryRepo.GetTargetConfig(r.Context(), chi.URLParam(r, "target"))
	if errors.Is(err, directory.ErrNotFound) {
		writeDetail(w, 404, "sync target not found")
		return
	}
	if err != nil {
		writeDetail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, v)
}
func decodeTargetConfigInput(w http.ResponseWriter, r *http.Request) (targetConfigInput, error) {
	var in targetConfigInput
	if r.Body == nil {
		return in, errors.New("invalid request body: expected JSON object")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var decoded *targetConfigInput
	if err := dec.Decode(&decoded); err != nil {
		return in, fmt.Errorf("invalid request body: %w", err)
	}
	if decoded == nil {
		return in, errors.New("invalid request body: expected JSON object")
	}
	in = *decoded
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return in, errors.New("invalid request body: multiple JSON values")
		}
		return in, fmt.Errorf("invalid request body: %w", err)
	}
	return in, nil
}

func (s *Server) updateSyncTargetConfig(w http.ResponseWriter, r *http.Request) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncManage)
	if caller == nil {
		return
	}
	target := chi.URLParam(r, "target")
	in, err := decodeTargetConfigInput(w, r)
	if err != nil {
		writeDetail(w, http.StatusBadRequest, err.Error())
		return
	}
	c := directory.TargetConfig{TargetCode: target, Enabled: in.Enabled, ScheduleType: in.ScheduleType, IntervalMinutes: in.IntervalMinutes, DailyTime: in.DailyTime, Timezone: in.Timezone}
	sc := directory.SyncConfig{Enabled: c.Enabled, ScheduleType: c.ScheduleType, IntervalMinutes: c.IntervalMinutes, DailyTime: c.DailyTime, Timezone: c.Timezone}
	if err := directory.ValidateSyncConfig(sc); err != nil {
		writeDetail(w, 400, err.Error())
		return
	}
	next, err := directory.NextTargetRunAt(c, time.Now().UTC())
	if err != nil {
		writeDetail(w, 400, err.Error())
		return
	}
	if err = s.DirectoryRepo.UpdateTargetConfig(r.Context(), target, c, caller.ID, next); err != nil {
		writeDetail(w, 400, err.Error())
		return
	}
	s.getSyncTargetConfig(w, r)
}
func (s *Server) createSyncTargetJob(w http.ResponseWriter, r *http.Request) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncManage)
	if caller == nil {
		return
	}
	job, err := s.DirectoryRepo.CreateTargetRun(r.Context(), chi.URLParam(r, "target"), "manual", &caller.ID, nil, "pending")
	if errors.Is(err, directory.ErrActiveJobExists) {
		writeDetail(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeDetail(w, 400, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
func decodeBatchInput(w http.ResponseWriter, r *http.Request) (batchInput, error) {
	var in batchInput
	if r.Body == nil {
		return in, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var decoded *batchInput
	if err := dec.Decode(&decoded); errors.Is(err, io.EOF) {
		return in, nil
	} else if err != nil {
		return in, fmt.Errorf("invalid request body: %w", err)
	}
	if decoded == nil {
		return in, errors.New("invalid request body: expected JSON object")
	}
	in = *decoded
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return in, errors.New("invalid request body: multiple JSON values")
		}
		return in, fmt.Errorf("invalid request body: %w", err)
	}
	if len(in.Targets) > 32 {
		return in, errors.New("targets must contain at most 32 entries")
	}
	return in, nil
}
func (s *Server) createSyncBatch(w http.ResponseWriter, r *http.Request) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncManage)
	if caller == nil {
		return
	}
	in, err := decodeBatchInput(w, r)
	if err != nil {
		writeDetail(w, http.StatusBadRequest, err.Error())
		return
	}
	batch, err := s.DirectoryRepo.CreateBatch(r.Context(), in.Targets, &caller.ID)
	if errors.Is(err, directory.ErrActiveJobExists) {
		writeDetail(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeDetail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, batch)
}

func (s *Server) listSyncJobs(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncRead) == nil {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	jobs, err := s.DirectoryRepo.ListTargetRuns(r.Context(), r.URL.Query().Get("target"), limit)
	if err != nil {
		writeDetail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, jobs)
}
func (s *Server) getSyncBatch(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncRead) == nil {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeDetail(w, 400, "invalid batch id")
		return
	}
	batch, err := s.DirectoryRepo.GetBatch(r.Context(), id)
	if errors.Is(err, directory.ErrNotFound) {
		writeDetail(w, 404, "sync batch not found")
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, batch)
}
