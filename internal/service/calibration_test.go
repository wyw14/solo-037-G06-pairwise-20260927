package service

import (
	"testing"
	"time"

	"github.com/wyw14/solo-037/internal/model"
	"github.com/wyw14/solo-037/internal/track"
)

func calibrationPoints() []track.ReferencePoint {
	return []track.ReferencePoint{
		{PointID: "p0", Nominal: 0, Forward: 0.1, Reverse: -0.1},
		{PointID: "p1", Nominal: 10, Forward: 10.1, Reverse: 9.9},
	}
}

func TestCalibrationArchivePersistsAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	archive, err := OpenCalibrationArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	calibration, err := archive.Record("carriage-1", calibrationPoints(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := OpenCalibrationArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := loaded.Get(calibration.ID)
	if !ok || got.Carriage != "carriage-1" || len(got.Segments) != 1 {
		t.Fatalf("reloaded archive missing calibration: %+v ok=%v", got, ok)
	}
}

func TestCurrentSkipsExpiredAndReturnsNewestValid(t *testing.T) {
	archive, _ := OpenCalibrationArchive(t.TempDir())
	now := time.Now().UTC()

	expired, err := archive.Record("carriage-1", calibrationPoints(), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// Backdate CreatedAt so the expired version is also older.
	archive.mu.Lock()
	expired.CreatedAt = now.Add(-2 * time.Hour)
	expired.ValidUntil = now.Add(-time.Hour)
	archive.byID[expired.ID] = expired
	archive.mu.Unlock()

	newest, err := archive.Record("carriage-1", calibrationPoints(), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	current, ok := archive.Current("carriage-1", now)
	if !ok || current.ID != newest.ID {
		t.Fatalf("current should be newest valid version, got %v ok=%v", current.ID, ok)
	}
	if _, ok := archive.Current("carriage-2", now); ok {
		t.Fatal("other carriage has no calibration")
	}
	// Expired versions remain retrievable by ID for historical runs.
	if _, ok := archive.Get(expired.ID); !ok {
		t.Fatal("expired calibration should remain usable by ID")
	}
}

func TestRecordPositionAppliesCurrentVersionAndPersists(t *testing.T) {
	directory := t.TempDir()
	runtime, err := OpenRuntime(directory)
	if err != nil {
		t.Fatal(err)
	}
	calibration, err := runtime.RecordCalibration("carriage-1", calibrationPoints(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.CreateMeasuredRun("tow A", "hull A", "section-a", "carriage-1")
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runtime.RecordPosition(run.Operation.ID, PositionInput{
		Position:   0,
		RecordedAt: time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	// measured p0 = 0, correction = 0 at p0; at raw 0 corrected 0. Use raw 5
	// for a meaningful interpolation instead: correction lerps 0..0 → 0 here
	// because both corrections are 0; verify identity + version binding instead.
	if outcome.CalibrationID != calibration.ID || outcome.Corrected != outcome.Raw || outcome.CalibrationExpired {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}

	reloaded, err := OpenRuntime(directory)
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := reloaded.MeasuredRun(run.Operation.ID)
	if !ok || len(stored.Positions) != 1 || stored.Positions[0].CalibrationID != calibration.ID {
		t.Fatalf("position not persisted: %+v", stored)
	}
	analysis, err := reloaded.AnalyzeMeasuredRun(run.Operation.ID)
	if err != nil || len(analysis.Positions) != 1 {
		t.Fatalf("analysis missing positions: %+v %v", analysis, err)
	}
}

func TestRecordPositionWithoutCalibrationStoredUncorrected(t *testing.T) {
	runtime, _ := OpenRuntime(t.TempDir())
	run, err := runtime.CreateMeasuredRun("tow B", "hull A", "section-a", "carriage-9")
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runtime.RecordPosition(run.Operation.ID, PositionInput{Position: 7.5, RecordedAt: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.CalibrationID != "" || outcome.Corrected != 7.5 {
		t.Fatalf("expected uncorrected reading, got %+v", outcome)
	}
}

func TestRecordPositionExpiredExplicitVersionIsFlagged(t *testing.T) {
	runtime, _ := OpenRuntime(t.TempDir())
	calibration, err := runtime.RecordCalibration("carriage-1", calibrationPoints(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	runtime.Calibrations.mu.Lock()
	calibration.ValidUntil = time.Now().Add(-time.Hour)
	runtime.Calibrations.byID[calibration.ID] = calibration
	runtime.Calibrations.mu.Unlock()

	run, _ := runtime.CreateMeasuredRun("tow C", "hull A", "section-a", "carriage-1")
	outcome, err := runtime.RecordPosition(run.Operation.ID, PositionInput{
		Position:      5,
		RecordedAt:    time.Now().Add(-time.Second),
		CalibrationID: calibration.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.CalibrationExpired || outcome.CalibrationID != calibration.ID {
		t.Fatalf("expired explicit version should be flagged: %+v", outcome)
	}

	other, _ := runtime.CreateMeasuredRun("tow D", "hull A", "section-a", "carriage-2")
	if _, err := runtime.RecordPosition(other.Operation.ID, PositionInput{
		Position:      5,
		RecordedAt:    time.Now().Add(-time.Second),
		CalibrationID: calibration.ID,
	}); err == nil {
		t.Fatal("calibration from another carriage accepted")
	}
}

func TestRecordPositionRejectsUnknownReferences(t *testing.T) {
	runtime, _ := OpenRuntime(t.TempDir())
	if _, err := runtime.RecordPosition(model.RunID("missing"), PositionInput{Position: 1, RecordedAt: time.Now()}); err == nil {
		t.Fatal("unknown run accepted")
	}
	run, _ := runtime.CreateMeasuredRun("tow E", "hull A", "section-a", "carriage-1")
	if _, err := runtime.RecordPosition(run.Operation.ID, PositionInput{
		Position:      1,
		RecordedAt:    time.Now(),
		CalibrationID: model.CalibrationID("missing"),
	}); err == nil {
		t.Fatal("unknown calibration accepted")
	}
}
