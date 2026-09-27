package track

import (
	"math"
	"testing"
	"time"
)

func samplePoints() []ReferencePoint {
	return []ReferencePoint{
		{PointID: "p0", Nominal: 0.0, Forward: 0.10, Reverse: 0.00},
		{PointID: "p1", Nominal: 10.0, Forward: 10.10, Reverse: 9.90},
		{PointID: "p2", Nominal: 20.0, Forward: 20.20, Reverse: 19.80},
	}
}

func TestBuildAveragesDirectionsAndDerivesSegments(t *testing.T) {
	validUntil := time.Now().Add(24 * time.Hour)
	calibration, err := Build("carriage-1", samplePoints(), validUntil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(calibration.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(calibration.Segments))
	}
	segment := calibration.Segments[0]
	// p0 measured = (0.10+0.00)/2 = 0.05, correction = -0.05
	if segment.FromMeasured != 0.05 || segment.FromCorrection != -0.05 {
		t.Fatalf("first endpoint wrong: %+v", segment)
	}
	// p1 measured = 10.0, correction = 0.0; max backlash = 0.2
	if segment.ToMeasured != 10.0 || segment.ToCorrection != 0.0 || math.Abs(segment.MaxBacklash-0.2) > 1e-9 {
		t.Fatalf("second endpoint wrong: %+v", segment)
	}
}

func TestCorrectInterpolatesAndClamps(t *testing.T) {
	calibration, err := Build("carriage-1", samplePoints(), time.Now().Add(time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Midpoint of segment 0 (measured 0.05..10.0): correction lerps -0.05..0,
	// raw 5.025 (halfway) -> corrected 5.0.
	mid := calibration.Correct(5.025)
	if math.Abs(mid-5.0) > 1e-9 {
		t.Fatalf("interpolation wrong: got %v", mid)
	}
	// Endpoint correction carries the correction of the nearest point.
	if got := calibration.Correct(-100); math.Abs(got-(-100.05)) > 1e-9 {
		t.Fatalf("low clamp wrong: got %v", got)
	}
	if got := calibration.Correct(100); math.Abs(got-(100.0)) > 1e-9 {
		t.Fatalf("high clamp wrong: got %v", got)
	}
	if got := calibration.Correct(math.NaN()); !math.IsNaN(got) {
		t.Fatalf("non-finite raw should pass through, got %v", got)
	}
}

func TestExpired(t *testing.T) {
	validUntil := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	calibration, err := Build("carriage-1", samplePoints(), validUntil, validUntil.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !calibration.Expired(validUntil) || !calibration.Expired(validUntil.Add(time.Hour)) {
		t.Fatal("expected expired at and after valid_until")
	}
	if calibration.Expired(validUntil.Add(-time.Second)) {
		t.Fatal("expected valid just before valid_until")
	}
}

func TestBuildRejectsInvalidSurveys(t *testing.T) {
	valid := time.Now().Add(time.Hour)
	at := time.Now()
	cases := map[string]func() []ReferencePoint{
		"fewer than two points": func() []ReferencePoint {
			return samplePoints()[:1]
		},
		"blank point id": func() []ReferencePoint {
			points := samplePoints()
			points[1] = ReferencePoint{PointID: "  ", Nominal: 10, Forward: 10.1, Reverse: 9.9}
			return points
		},
		"non-finite value": func() []ReferencePoint {
			points := samplePoints()
			points[1] = ReferencePoint{PointID: "p1", Nominal: 10, Forward: math.Inf(1), Reverse: 9.9}
			return points
		},
		"nominal not increasing": func() []ReferencePoint {
			points := samplePoints()
			points[1] = ReferencePoint{PointID: "p1", Nominal: 0, Forward: 10.1, Reverse: 9.9}
			return points
		},
		"measured not increasing": func() []ReferencePoint {
			points := samplePoints()
			points[1] = ReferencePoint{PointID: "p1", Nominal: 10, Forward: -10.1, Reverse: -9.9}
			return points
		},
	}
	for name, mutate := range cases {
		if _, err := Build("carriage-1", mutate(), valid, at); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := Build("  ", samplePoints(), valid, at); err == nil {
		t.Fatal("blank carriage accepted")
	}
	if _, err := Build("carriage-1", samplePoints(), at, at); err == nil {
		t.Fatal("past valid_until accepted")
	}
}
