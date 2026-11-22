package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ScheduleSpec is one cron-expressed schedule (config/schedules/*.yaml).
// The scheduler runtime fires GoalRequest runs on its cadence.
type ScheduleSpec struct {
	Name     string `yaml:"name"`
	Schedule string `yaml:"schedule"` // cron 5-field or @descriptor
	Goal     string `yaml:"goal"`
	Agent    string `yaml:"agent,omitempty"`
	Model    string `yaml:"model,omitempty"`
	// MissedPolicy: "skip" (default) forgets fires missed while the
	// daemon was down; "catch_up" fires once on wake.
	MissedPolicy string `yaml:"missed_policy,omitempty"`

	Path string `yaml:"-"`
}

// scheduleCapacity bounds the schedules plane; overflow surfaces as load
// errors so the API list stays complete within the bound.
const scheduleCapacity = 128

// loadSchedules loads config/schedules/*.yaml with per-file error
// isolation; a missing directory is an empty plane.
func loadSchedules(snap *Snapshot, dir string, errs *[]LoadError) {
	n := 0
	for _, e := range readKindDir(dir, KindSchedules, errs) {
		if len(snap.Schedules) >= scheduleCapacity {
			addErr(errs, filepath.Join(dir, e.Name()), KindSchedules, fmt.Errorf("schedules capacity %d exceeded", scheduleCapacity))
			continue
		}
		if e.IsDir() || skipEntry(e.Name()) || !isYAML(e.Name()) {
			continue
		}
		path := dir + "/" + e.Name()
		raw, err := os.ReadFile(path)
		if err != nil {
			addErr(errs, path, KindSchedules, err)
			continue
		}
		var spec ScheduleSpec
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(&spec); err != nil {
			addErr(errs, path, KindSchedules, fmt.Errorf("decode: %w", err))
			continue
		}
		if spec.Name == "" || spec.Schedule == "" || spec.Goal == "" {
			addErr(errs, path, KindSchedules, fmt.Errorf("name, schedule, and goal are required"))
			continue
		}
		if spec.MissedPolicy == "" {
			spec.MissedPolicy = "skip"
		}
		if spec.MissedPolicy != "skip" && spec.MissedPolicy != "catch_up" {
			addErr(errs, path, KindSchedules, fmt.Errorf("missed_policy must be skip or catch_up"))
			continue
		}
		spec.Path = path
		n++
		if existing, dup := snap.Schedules[spec.Name]; dup {
			addErr(errs, path, KindSchedules, fmt.Errorf("duplicate schedule name %q (keeping %s)", spec.Name, existing.Path))
			continue
		}
		snap.Schedules[spec.Name] = spec
	}
}

// CreateSchedule validates and installs a schedule file atomically
// (no-clobber). The expression must parse.
func (r *Registry) CreateSchedule(spec ScheduleSpec) (ScheduleSpec, error) {
	if err := validateSchedule(spec); err != nil {
		return ScheduleSpec{}, err
	}
	raw, err := marshalSchedule(spec)
	if err != nil {
		return ScheduleSpec{}, mutationErr(ErrInvalidManifest, "encode: %v", err)
	}
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	root, err := r.kindDirLocked(KindSchedules)
	if err != nil {
		return ScheduleSpec{}, err
	}
	installed, err := installNoClobber(root, spec.Name+".yaml", raw)
	if err != nil {
		return ScheduleSpec{}, mutationErr(ErrDuplicateName, "the target file %s already exists", installed)
	}
	spec.Path = installed
	r.reloadKindLocked(KindSchedules)
	r.notifyKindDiff(KindSchedules, nil, nil)
	return spec, nil
}

// UpdateSchedule replaces a schedule's file (rename-aware: newName may
// differ, in which case the old file is removed after the new installs).
func (r *Registry) UpdateSchedule(name, newName string, spec ScheduleSpec) (ScheduleSpec, error) {
	if err := validateSchedule(spec); err != nil {
		return ScheduleSpec{}, err
	}
	raw, err := marshalSchedule(spec)
	if err != nil {
		return ScheduleSpec{}, mutationErr(ErrInvalidManifest, "encode: %v", err)
	}
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	root, err := r.kindDirLocked(KindSchedules)
	if err != nil {
		return ScheduleSpec{}, err
	}
	if newName == name {
		if err := replaceFile(filepath.Join(root, name+".yaml"), raw); err != nil {
			return ScheduleSpec{}, mutationErr(ErrIO, "replace: %v", err)
		}
	} else {
		// Rename-aware: install the NEW file no-clobber, then remove the
		// old one (order keeps a crash from losing the schedule).
		installed, err := installNoClobber(root, newName+".yaml", raw)
		if err != nil {
			return ScheduleSpec{}, mutationErr(ErrDuplicateName, "the target file %s already exists", installed)
		}
		if err := os.Remove(filepath.Join(root, name+".yaml")); err != nil {
			return ScheduleSpec{}, mutationErr(ErrIO, "remove old name: %v", err)
		}
		spec.Path = installed
	}
	r.reloadKindLocked(KindSchedules)
	r.notifyKindDiff(KindSchedules, nil, nil)
	return spec, nil
}

// DeleteSchedule removes a schedule's file.
func (r *Registry) DeleteSchedule(name string) error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	root, err := r.kindDirLocked(KindSchedules)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, name+".yaml")); err != nil {
		return mutationErr(ErrNotFound, "schedule %q is not configured", name)
	}
	if err := os.Remove(filepath.Join(root, name+".yaml")); err != nil {
		return mutationErr(ErrIO, "delete: %v", err)
	}
	r.reloadKindLocked(KindSchedules)
	r.notifyKindDiff(KindSchedules, nil, nil)
	return nil
}

func validateSchedule(spec ScheduleSpec) error {
	if spec.Name == "" || spec.Schedule == "" || spec.Goal == "" {
		return mutationErr(ErrInvalidName, "name, schedule, and goal are required")
	}
	if !validManagedName(spec.Name) {
		return mutationErr(ErrInvalidName, "name %q is not a valid managed name", spec.Name)
	}
	if len(spec.Goal) > 8192 {
		return mutationErr(ErrInvalidManifest, "goal exceeds 8192 bytes")
	}
	return nil
}

func marshalSchedule(spec ScheduleSpec) ([]byte, error) {
	out := struct {
		ScheduleSpec `yaml:",inline"`
	}{spec}
	return yaml.Marshal(out)
}
