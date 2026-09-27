package service

import (
	"math"
	"testing"
	"time"

	"github.com/wyw14/solo-037/internal/model"
)

func referenceInput(label string, nominal, forward, reverse float64) ReferenceInput {
	return ReferenceInput{
		Label:      label,
		Nominal:    nominal,
		Forward:    forward,
		Reverse:    reverse,
		RecordedBy: "operator-1",
		RecordedAt: time.Now().Add(-time.Minute).UTC(),
	}
}

func TestCalibrationArchivePersistsAndCorrects(t *testing.T) {
	directory := t.TempDir()
	a, err := OpenCalibrationArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []ReferenceInput{
		referenceInput("A", 10, 10.2, 10.0),
		referenceInput("B", 20, 20.4, 20.2),
	} {
		if _, err := a.RecordReferencePoint(input); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.BuildCalibration(0, time.Now()); err == nil {
		t.Fatal("zero validity accepted")
	}
	curve, err := a.BuildCalibration(24, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if curve.Version == "" || len(curve.Segments) != 1 || !curve.ExpiresAt.After(curve.CreatedAt) {
		t.Fatalf("bad curve: %+v", curve)
	}
	reloaded, err := OpenCalibrationArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ReferencePoints()) != 2 || len(reloaded.Calibrations()) != 1 {
		t.Fatalf("archive did not persist: %+v", reloaded.Calibrations())
	}
	correction, err := reloaded.Correct(15.2, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !correction.Calibrated || !correction.Valid || correction.Version != curve.Version {
		t.Fatalf("correction missing version or validity: %+v", correction)
	}
	if correction.Raw != 15.2 || math.Abs(correction.Corrected-15.0) > 1e-9 {
		t.Fatalf("correction wrong: %+v", correction)
	}
}

func TestCalibrationArchiveValidationAndPassthrough(t *testing.T) {
	a, err := OpenCalibrationArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	correction, err := a.Correct(7.5, time.Now())
	if err != nil || correction.Calibrated || correction.Corrected != 7.5 {
		t.Fatalf("expected uncalibrated passthrough, got %+v (%v)", correction, err)
	}
	if _, err := a.RecordReferencePoint(referenceInput("A", 10, 10.1, 9.9)); err != nil {
		t.Fatal(err)
	}
	bad := referenceInput("B", 20, math.NaN(), 20)
	if _, err := a.RecordReferencePoint(bad); err == nil {
		t.Fatal("NaN reading accepted")
	}
	bad = referenceInput("C", 30, 30.1, 29.9)
	bad.RecordedBy = " "
	if _, err := a.RecordReferencePoint(bad); err == nil {
		t.Fatal("missing operator accepted")
	}
	if _, err := a.BuildCalibration(1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Correct(math.Inf(1), time.Now()); err == nil {
		t.Fatal("infinite position accepted")
	}
}

func TestMeasuredRunAnalysisShowsCorrectedPositions(t *testing.T) {
	runtime, err := OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RecordReferencePoint(referenceInput("A", 10, 10.2, 10.0)); err != nil {
		t.Fatal(err)
	}
	curve, err := runtime.BuildTrackCalibration(24)
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.CreateMeasuredRun("tow run", "hull X", "section-a", "carriage-1")
	if err != nil {
		t.Fatal(err)
	}
	// Evidence recorded after the calibration was built falls inside its
	// validity window.
	at := time.Now().UTC()
	if _, err := runtime.RecordMeasurement(run.Operation.ID, MeasurementInput{Kind: model.EvidencePosition, SourceID: "rail-scale", Value: 12.3, RecordedAt: at}); err != nil {
		t.Fatal(err)
	}
	analysis, err := runtime.AnalyzeMeasuredRun(run.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Positions) != 1 {
		t.Fatalf("expected one corrected position, got %+v", analysis.Positions)
	}
	position := analysis.Positions[0]
	if position.Raw != 12.3 || math.Abs(position.Corrected-12.2) > 1e-9 {
		t.Fatalf("raw/corrected mismatch: %+v", position)
	}
	if position.Version != curve.Version || !position.Calibrated || !position.Valid {
		t.Fatalf("calibration version not visible: %+v", position)
	}
}
