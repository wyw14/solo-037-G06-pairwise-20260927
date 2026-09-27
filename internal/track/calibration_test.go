package track

import (
	"math"
	"testing"
	"time"
)

func surveyedPoints() []ReferencePoint {
	at := time.Unix(1700000000, 0).UTC()
	return []ReferencePoint{
		NewReferencePoint("B", 20, 20.4, 20.2, "operator", at),
		NewReferencePoint("A", 10, 10.2, 10.0, "operator", at),
		NewReferencePoint("C", 30, 29.8, 29.6, "operator", at),
	}
}

func TestCalibrationBuildsSortedSegments(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	curve, err := NewCalibration(surveyedPoints(), "v1", created, created.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(curve.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %+v", curve.Segments)
	}
	if math.Abs(curve.Segments[0].Start-10.1) > 1e-9 || math.Abs(curve.Segments[1].Start-20.3) > 1e-9 {
		t.Fatalf("segments not ordered by mean reading: %+v", curve.Segments)
	}
	point := curve.Points[0]
	if point.Label != "A" || math.Abs(point.Backlash()-0.2) > 1e-9 || math.Abs(point.Correction()+0.1) > 1e-9 {
		t.Fatalf("unexpected point metrics: %+v", point)
	}
}

func TestCalibrationCorrectsPiecewiseAndClamps(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	curve, err := NewCalibration(surveyedPoints(), "v1", created, created.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Midpoint of the first segment interpolates between -0.1 and -0.3.
	if got := curve.Correct(15.2); math.Abs(got-15.0) > 1e-9 {
		t.Fatalf("interpolated correction wrong: %v", got)
	}
	// Below the surveyed span the nearest endpoint correction applies.
	if got := curve.Correct(0); math.Abs(got-(-0.1)) > 1e-9 {
		t.Fatalf("low clamp wrong: %v", got)
	}
	// Above the span the last endpoint correction (+0.3) applies.
	if got := curve.Correct(40); math.Abs(got-40.3) > 1e-9 {
		t.Fatalf("high clamp wrong: %v", got)
	}
}

func TestCalibrationValidityWindow(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	curve, err := NewCalibration(surveyedPoints(), "v1", created, created.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !curve.Valid(created.Add(30 * time.Minute)) {
		t.Fatal("curve should be valid inside its window")
	}
	if curve.Valid(created.Add(2*time.Hour)) || curve.Valid(created.Add(-time.Minute)) {
		t.Fatal("curve valid outside its window")
	}
	applied := curve.Apply(15.2, created.Add(30*time.Minute))
	if !applied.Calibrated || !applied.Valid || applied.Version != "v1" || applied.ExpiresAt != curve.ExpiresAt {
		t.Fatalf("apply did not stamp version and validity: %+v", applied)
	}
	if got := Uncorrected(5); got.Calibrated || got.Corrected != 5 || got.Version != "" {
		t.Fatalf("uncorrected passthrough wrong: %+v", got)
	}
}

func TestCalibrationRejectsInvalidInput(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	if _, err := NewCalibration(nil, "v1", created, created.Add(time.Hour)); err == nil {
		t.Fatal("empty points accepted")
	}
	if _, err := NewCalibration(surveyedPoints(), "", created, created.Add(time.Hour)); err == nil {
		t.Fatal("empty version accepted")
	}
	if _, err := NewCalibration(surveyedPoints(), "v1", created, created); err == nil {
		t.Fatal("non-positive validity accepted")
	}
}

func TestSinglePointCalibrationIsConstant(t *testing.T) {
	created := time.Unix(1700000000, 0).UTC()
	curve, err := NewCalibration(surveyedPoints()[:1], "v1", created, created.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	offset := surveyedPoints()[0].Correction()
	if got := curve.Correct(100); math.Abs(got-(100+offset)) > 1e-9 {
		t.Fatalf("single point correction not constant: %v", got)
	}
}
