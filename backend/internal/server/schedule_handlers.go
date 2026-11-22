package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"zbot/backend/internal/registry"
	"zbot/backend/internal/scheduler"
	apigen "zbot/backend/internal/server/apigen"
)

// ScheduleSource is the scheduler surface the handlers read (next fire,
// last outcome) without importing the runtime directly.
type ScheduleSource interface {
	NextFire(expr string, after time.Time) (time.Time, error)
	LastOutcome(name string) (scheduler.Outcome, bool)
}

func scheduleView(spec registry.ScheduleSpec, src ScheduleSource) apigen.Schedule {
	v := apigen.Schedule{
		Name: spec.Name, Schedule: spec.Schedule, Goal: spec.Goal,
		Agent: nilStrPtr(spec.Agent), Model: nilStrPtr(spec.Model),
		MissedPolicy: (*apigen.ScheduleMissedPolicy)(nilStrPtr(spec.MissedPolicy)),
		Source:       nilStrPtr(spec.Path),
	}
	if next, err := src.NextFire(spec.Schedule, time.Now()); err == nil {
		t := next.UTC()
		v.NextFire = &t
	}
	if o, ok := src.LastOutcome(spec.Name); ok {
		status := apigen.ScheduleLastOutcome(o.Status)
		v.LastOutcome = &status
		at := o.At.UTC()
		v.LastOutcomeAt = &at
	}
	return v
}

// ListSchedules lists the roster with live next-fire and outcomes.
func (s *Server) ListSchedules(w http.ResponseWriter, r *http.Request) {
	snap := s.reg.Snapshot()
	names := make([]string, 0, len(snap.Schedules))
	for name := range snap.Schedules {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]apigen.Schedule, 0, len(names))
	for _, name := range names {
		out = append(out, scheduleView(snap.Schedules[name], s.sched))
	}
	writeJSON(w, http.StatusOK, apigen.ScheduleList{Schedules: out})
}

func scheduleFromCreate(req apigen.ScheduleCreate) registry.ScheduleSpec {
	missed := "skip"
	if req.MissedPolicy != nil {
		missed = string(*req.MissedPolicy)
	}
	return registry.ScheduleSpec{
		Name: req.Name, Schedule: req.Schedule, Goal: req.Goal,
		Agent: strOr(req.Agent), Model: strOr(req.Model), MissedPolicy: missed,
	}
}

// CreateSchedule installs a schedule (typed 400s for invalid shapes and
// expressions — nothing written on failure).
func (s *Server) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req apigen.ScheduleCreate
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	spec := scheduleFromCreate(req)
	if err := scheduler.ValidExpression(spec.Schedule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_schedule", err.Error())
		return
	}
	created, err := s.reg.CreateSchedule(spec)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduleView(created, s.sched))
}

// GetSchedule reads one schedule.
func (s *Server) GetSchedule(w http.ResponseWriter, r *http.Request, name string) {
	snap := s.reg.Snapshot()
	spec, ok := snap.Schedules[string(name)]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("schedule %q is not configured", name))
		return
	}
	writeJSON(w, http.StatusOK, scheduleView(spec, s.sched))
}

// UpdateSchedule replaces a schedule (rename-aware).
func (s *Server) UpdateSchedule(w http.ResponseWriter, r *http.Request, name string) {
	var req apigen.ScheduleCreate
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	spec := scheduleFromCreate(req)
	if err := scheduler.ValidExpression(spec.Schedule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_schedule", err.Error())
		return
	}
	updated, err := s.reg.UpdateSchedule(string(name), spec.Name, spec)
	if err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduleView(updated, s.sched))
}

// DeleteSchedule removes a schedule.
func (s *Server) DeleteSchedule(w http.ResponseWriter, r *http.Request, name string) {
	if err := s.reg.DeleteSchedule(string(name)); err != nil {
		writeScheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apigen.ScheduleDeleted{Deleted: string(name)})
}

func writeScheduleError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invalid") || strings.Contains(msg, "required"):
		writeError(w, http.StatusBadRequest, "invalid_schedule", msg)
	case strings.Contains(msg, "duplicate"):
		writeError(w, http.StatusBadRequest, "duplicate_name", msg)
	case strings.Contains(msg, "not configured"):
		writeError(w, http.StatusNotFound, "not_found", msg)
	default:
		writeError(w, http.StatusInternalServerError, "io_error", msg)
	}
}

func strOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
