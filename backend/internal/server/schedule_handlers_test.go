package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zbot/backend/internal/registry"
	"zbot/backend/internal/scheduler"
)

type fakeSched struct{}

func (fakeSched) NextFire(string, time.Time) (time.Time, error) {
	return time.Date(2026, 11, 23, 7, 0, 0, 0, time.UTC), nil
}
func (fakeSched) LastOutcome(string) (scheduler.Outcome, bool) {
	return scheduler.Outcome{At: time.Date(2026, 11, 22, 7, 0, 0, 0, time.UTC), Status: "fired"}, true
}

func newScheduleServer(t *testing.T) (*httptest.Server, *registry.Registry) {
	t.Helper()
	dir := t.TempDir()
	reg := registry.New()
	reg.LoadAll(registry.Dirs{Schedules: dir})
	srv := New(nil, nil, nil, nil, nil, nil, nil, reg, nil, fakeSched{}, "test")
	return httptest.NewServer(srv.Routes()), reg
}

func TestScheduleCRUDRoundTrip(t *testing.T) {
	t.Parallel()
	daemon, _ := newScheduleServer(t)
	defer daemon.Close()

	// Create with a valid expression.
	body, _ := json.Marshal(map[string]any{"name": "digest", "schedule": "0 7 * * *", "goal": "Summarize the day.", "agent": "research-agent"})
	res, err := http.Post(daemon.URL+"/api/schedules", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Name     string `json:"name"`
		NextFire string `json:"next_fire"`
		Agent    string `json:"agent"`
	}
	json.NewDecoder(res.Body).Decode(&created)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || created.Name != "digest" || created.NextFire == "" {
		t.Fatalf("create wrong: %d %+v", res.StatusCode, created)
	}

	// List shows it with next_fire + last outcome.
	res, _ = http.Get(daemon.URL + "/api/schedules")
	var list struct{ Schedules []map[string]any }
	json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list.Schedules) != 1 || list.Schedules[0]["next_fire"] == nil || list.Schedules[0]["last_outcome"] != "fired" {
		t.Fatalf("list wrong: %+v", list)
	}

	// Invalid expression: typed 400, nothing written.
	bad, _ := json.Marshal(map[string]any{"name": "bad", "schedule": "not a cron", "goal": "G"})
	res, _ = http.Post(daemon.URL+"/api/schedules", "application/json", bytes.NewReader(bad))
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid expression must 400, got %d", res.StatusCode)
	}

	// Update (rename-aware).
	upd, _ := json.Marshal(map[string]any{"name": "digest2", "schedule": "@daily", "goal": "Summarize."})
	req, _ := http.NewRequest(http.MethodPut, daemon.URL+"/api/schedules/digest", bytes.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update: %d", res.StatusCode)
	}

	// Old name 404s, new name reads.
	res, _ = http.Get(daemon.URL + "/api/schedules/digest")
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("old name should 404: %d", res.StatusCode)
	}
	res, _ = http.Get(daemon.URL + "/api/schedules/digest2")
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("new name should read: %d", res.StatusCode)
	}

	// Delete.
	req, _ = http.NewRequest(http.MethodDelete, daemon.URL+"/api/schedules/digest2", nil)
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	res, _ = http.Get(daemon.URL + "/api/schedules")
	var after struct{ Schedules []map[string]any }
	json.NewDecoder(res.Body).Decode(&after)
	res.Body.Close()
	if len(after.Schedules) != 0 {
		t.Fatalf("roster not empty after delete")
	}
	_ = os.RemoveAll(filepath.Dir(""))
}
