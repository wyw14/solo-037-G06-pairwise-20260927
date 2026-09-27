package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wyw14/solo-037/internal/model"
	"github.com/wyw14/solo-037/internal/track"
)

// ReferenceInput is one operator-surveyed benchmark reading.
type ReferenceInput struct {
	Label      string    `json:"label"`
	Nominal    float64   `json:"nominal"`
	Forward    float64   `json:"forward"`
	Reverse    float64   `json:"reverse"`
	RecordedBy string    `json:"recorded_by"`
	RecordedAt time.Time `json:"recorded_at"`
}

type calibrationState struct {
	Points       []track.ReferencePoint `json:"points"`
	Calibrations []track.Calibration    `json:"calibrations"`
}

// CalibrationArchive keeps surveyed reference points and the versioned
// correction curves built from them, persisted next to the measurements.
type CalibrationArchive struct {
	mu     sync.Mutex
	path   string
	points []track.ReferencePoint
	curves []track.Calibration
}

func OpenCalibrationArchive(directory string) (*CalibrationArchive, error) {
	a := &CalibrationArchive{}
	if directory == "" {
		return a, nil
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, err
	}
	a.path = filepath.Join(directory, "track-calibration.json")
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	var state calibrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("read calibration archive: %w", err)
	}
	a.points = state.Points
	a.curves = state.Calibrations
	return a, nil
}

func (a *CalibrationArchive) commit() error {
	if a.path != "" {
		data, err := json.MarshalIndent(calibrationState{Points: a.points, Calibrations: a.curves}, "", "  ")
		if err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(a.path), "calibration-*.tmp")
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
	return nil
}

func finite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

// RecordReferencePoint stores one operator-surveyed benchmark reading.
func (a *CalibrationArchive) RecordReferencePoint(input ReferenceInput) (track.ReferencePoint, error) {
	if strings.TrimSpace(input.RecordedBy) == "" {
		return track.ReferencePoint{}, errors.New("recording operator is required")
	}
	if !finite(input.Nominal, input.Forward, input.Reverse) {
		return track.ReferencePoint{}, errors.New("nominal, forward and reverse positions must be finite")
	}
	if input.RecordedAt.IsZero() || input.RecordedAt.After(time.Now().Add(time.Minute)) {
		return track.ReferencePoint{}, errors.New("a valid observation time is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	point := track.NewReferencePoint(strings.TrimSpace(input.Label), input.Nominal, input.Forward, input.Reverse, strings.TrimSpace(input.RecordedBy), input.RecordedAt)
	a.points = append(a.points, point)
	sort.Slice(a.points, func(i, j int) bool { return a.points[i].Nominal < a.points[j].Nominal })
	if err := a.commit(); err != nil {
		return track.ReferencePoint{}, err
	}
	return point, nil
}

func (a *CalibrationArchive) ReferencePoints() []track.ReferencePoint {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]track.ReferencePoint(nil), a.points...)
}

// BuildCalibration freezes the current reference points into a new
// versioned correction curve valid for validHours from now.
func (a *CalibrationArchive) BuildCalibration(validHours float64, at time.Time) (track.Calibration, error) {
	if !finite(validHours) || validHours <= 0 {
		return track.Calibration{}, errors.New("a positive validity period in hours is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	at = at.UTC()
	curve, err := track.NewCalibration(a.points, string(model.NewEventID()), at, at.Add(time.Duration(validHours*float64(time.Hour))))
	if err != nil {
		return track.Calibration{}, err
	}
	a.curves = append(a.curves, curve)
	if err := a.commit(); err != nil {
		return track.Calibration{}, err
	}
	return curve, nil
}

func (a *CalibrationArchive) Calibrations() []track.Calibration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]track.Calibration(nil), a.curves...)
}

// CurrentCalibration returns the most recently built curve.
func (a *CalibrationArchive) CurrentCalibration() (track.Calibration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.curves) == 0 {
		return track.Calibration{}, false
	}
	return a.curves[len(a.curves)-1], true
}

// Correct applies the current curve to a raw scale reading. Without any
// calibration the reading passes through unmodified with Calibrated=false.
func (a *CalibrationArchive) Correct(position float64, at time.Time) (track.Correction, error) {
	if !finite(position) {
		return track.Correction{}, errors.New("position must be finite")
	}
	curve, ok := a.CurrentCalibration()
	if !ok {
		return track.Uncorrected(position), nil
	}
	return curve.Apply(position, at), nil
}

func (r *Runtime) RecordReferencePoint(input ReferenceInput) (track.ReferencePoint, error) {
	return r.Calibrations.RecordReferencePoint(input)
}

func (r *Runtime) ReferencePoints() []track.ReferencePoint { return r.Calibrations.ReferencePoints() }

func (r *Runtime) BuildTrackCalibration(validHours float64) (track.Calibration, error) {
	return r.Calibrations.BuildCalibration(validHours, time.Now())
}

func (r *Runtime) TrackCalibrations() []track.Calibration { return r.Calibrations.Calibrations() }

func (r *Runtime) CorrectTrackPosition(position float64, at time.Time) (track.Correction, error) {
	return r.Calibrations.Correct(position, at)
}
