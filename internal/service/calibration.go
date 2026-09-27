package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/wyw14/solo-037/internal/model"
	"github.com/wyw14/solo-037/internal/track"
)

// CalibrationArchive persists surveyed track calibrations. Versions are
// committed to disk before they are published in memory and are reloaded on
// restart, mirroring the measurement archive.
type CalibrationArchive struct {
	mu   sync.Mutex
	path string
	byID map[model.CalibrationID]track.Calibration
}

func OpenCalibrationArchive(directory string) (*CalibrationArchive, error) {
	a := &CalibrationArchive{byID: make(map[model.CalibrationID]track.Calibration)}
	if directory == "" {
		return a, nil
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, err
	}
	a.path = filepath.Join(directory, "track-calibrations.json")
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.byID); err != nil {
		return nil, fmt.Errorf("read calibration archive: %w", err)
	}
	if a.byID == nil {
		return nil, errors.New("calibration archive must contain a version map")
	}
	return a, nil
}

func (a *CalibrationArchive) commit(next map[model.CalibrationID]track.Calibration) error {
	if a.path != "" {
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(a.path), "calibrations-*.tmp")
		if err != nil {
			return err
		}
		name := file.Name()
		defer os.Remove(name)
		if _, err = file.Write(data); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Rename(name, a.path); err != nil {
			return err
		}
	}
	a.byID = next
	return nil
}

// Record validates the survey, builds the correction curve and commits a new
// calibration version for the carriage.
func (a *CalibrationArchive) Record(carriage string, points []track.ReferencePoint, validUntil time.Time) (track.Calibration, error) {
	calibration, err := track.Build(carriage, points, validUntil, time.Now().UTC())
	if err != nil {
		return track.Calibration{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := make(map[model.CalibrationID]track.Calibration, len(a.byID)+1)
	for id, value := range a.byID {
		next[id] = value
	}
	next[calibration.ID] = calibration
	if err := a.commit(next); err != nil {
		return track.Calibration{}, err
	}
	return calibration, nil
}

func (a *CalibrationArchive) Get(id model.CalibrationID) (track.Calibration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	calibration, exists := a.byID[id]
	return calibration, exists
}

func (a *CalibrationArchive) List() []track.Calibration {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]track.Calibration, 0, len(a.byID))
	for _, calibration := range a.byID {
		result = append(result, calibration)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result
}

// Current returns the newest calibration for the carriage that is still
// within its validity period at the given time.
func (a *CalibrationArchive) Current(carriage string, at time.Time) (track.Calibration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var current track.Calibration
	found := false
	for _, calibration := range a.byID {
		if calibration.Carriage != carriage || calibration.Expired(at) {
			continue
		}
		if !found || calibration.CreatedAt.After(current.CreatedAt) {
			current = calibration
			found = true
		}
	}
	return current, found
}

// PositionInput is one operator-supplied carriage position observation. An
// explicit CalibrationID selects a specific version (expired versions stay
// usable and are flagged in the outcome); otherwise the carriage's current
// valid version is applied.
type PositionInput struct {
	Position      float64             `json:"position"`
	RecordedAt    time.Time           `json:"recorded_at"`
	CalibrationID model.CalibrationID `json:"calibration_id,omitempty"`
}

// PositionOutcome is the committed reading plus whether the applied
// calibration was already past its validity period.
type PositionOutcome struct {
	PositionReading
	CalibrationExpired bool `json:"calibration_expired"`
}

func (r *Runtime) RecordCalibration(carriage string, points []track.ReferencePoint, validUntil time.Time) (track.Calibration, error) {
	return r.Calibrations.Record(carriage, points, validUntil)
}

func (r *Runtime) ListCalibrations() []track.Calibration { return r.Calibrations.List() }

func (r *Runtime) Calibration(id model.CalibrationID) (track.Calibration, bool) {
	return r.Calibrations.Get(id)
}

// RecordPosition corrects a raw carriage position with the selected
// calibration and commits it to the measured run. Without any usable
// calibration the reading is stored uncorrected with an empty version.
func (r *Runtime) RecordPosition(id model.RunID, input PositionInput) (PositionOutcome, error) {
	run, exists := r.Measurements.Get(id)
	if !exists {
		return PositionOutcome{}, errors.New("run not found")
	}
	now := time.Now().UTC()
	reading := PositionReading{Raw: input.Position, Corrected: input.Position, RecordedAt: input.RecordedAt}
	expired := false
	if input.CalibrationID != "" {
		calibration, ok := r.Calibrations.Get(input.CalibrationID)
		if !ok {
			return PositionOutcome{}, errors.New("calibration not found")
		}
		if calibration.Carriage != run.Operation.Carriage {
			return PositionOutcome{}, errors.New("calibration carriage does not match run carriage")
		}
		reading.Corrected = calibration.Correct(input.Position)
		reading.CalibrationID = calibration.ID
		expired = calibration.Expired(now)
	} else if calibration, ok := r.Calibrations.Current(run.Operation.Carriage, now); ok {
		reading.Corrected = calibration.Correct(input.Position)
		reading.CalibrationID = calibration.ID
	}
	saved, err := r.Measurements.AppendPosition(id, reading)
	if err != nil {
		return PositionOutcome{}, err
	}
	return PositionOutcome{PositionReading: saved, CalibrationExpired: expired}, nil
}
