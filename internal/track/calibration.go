package track

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/wyw14/solo-037/internal/model"
)

// ReferencePoint is one surveyed benchmark on the rail: the nominal (true)
// position plus the positions the carriage scale reported while travelling
// in each direction. Averaging both directions cancels drive backlash.
type ReferencePoint struct {
	ID         model.EventID `json:"id"`
	Label      string        `json:"label"`
	Nominal    float64       `json:"nominal"`
	Forward    float64       `json:"forward"`
	Reverse    float64       `json:"reverse"`
	RecordedBy string        `json:"recorded_by"`
	RecordedAt time.Time     `json:"recorded_at"`
}

func NewReferencePoint(label string, nominal, forward, reverse float64, recordedBy string, at time.Time) ReferencePoint {
	return ReferencePoint{
		ID:         model.NewEventID(),
		Label:      label,
		Nominal:    nominal,
		Forward:    forward,
		Reverse:    reverse,
		RecordedBy: recordedBy,
		RecordedAt: at.UTC(),
	}
}

// MeanReading is the backlash-free scale reading at this benchmark.
func (p ReferencePoint) MeanReading() float64 { return (p.Forward + p.Reverse) / 2 }

// Backlash is the spread between the two travel directions.
func (p ReferencePoint) Backlash() float64 { return math.Abs(p.Forward - p.Reverse) }

// Correction is the offset that maps the scale reading back to nominal.
func (p ReferencePoint) Correction() float64 { return p.Nominal - p.MeanReading() }

// Segment is the correction span between two neighbouring reference points.
type Segment struct {
	Start           float64 `json:"start"`
	End             float64 `json:"end"`
	StartCorrection float64 `json:"start_correction"`
	EndCorrection   float64 `json:"end_correction"`
}

func (s Segment) Contains(position float64) bool {
	return position >= s.Start && position <= s.End
}

// Correction interpolates the offset linearly across the segment.
func (s Segment) Correction(position float64) float64 {
	if s.End <= s.Start {
		return s.StartCorrection
	}
	ratio := (position - s.Start) / (s.End - s.Start)
	return s.StartCorrection + ratio*(s.EndCorrection-s.StartCorrection)
}

// Calibration is a versioned, segmented correction curve that expires.
type Calibration struct {
	Version   string           `json:"version"`
	Points    []ReferencePoint `json:"points"`
	Segments  []Segment        `json:"segments"`
	CreatedAt time.Time        `json:"created_at"`
	ExpiresAt time.Time        `json:"expires_at"`
}

// NewCalibration sorts the surveyed points by mean reading and builds one
// segment per neighbouring pair. With a single point the correction is
// constant across the whole rail.
func NewCalibration(points []ReferencePoint, version string, created, expires time.Time) (Calibration, error) {
	if len(points) == 0 {
		return Calibration{}, errors.New("at least one reference point is required")
	}
	if version == "" {
		return Calibration{}, errors.New("calibration version is required")
	}
	created = created.UTC()
	expires = expires.UTC()
	if !expires.After(created) {
		return Calibration{}, errors.New("calibration must expire after it is created")
	}
	ordered := append([]ReferencePoint(nil), points...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].MeanReading() < ordered[j].MeanReading() })
	segments := make([]Segment, 0, len(ordered)-1)
	for i := 0; i+1 < len(ordered); i++ {
		segments = append(segments, Segment{
			Start:           ordered[i].MeanReading(),
			End:             ordered[i+1].MeanReading(),
			StartCorrection: ordered[i].Correction(),
			EndCorrection:   ordered[i+1].Correction(),
		})
	}
	return Calibration{Version: version, Points: ordered, Segments: segments, CreatedAt: created, ExpiresAt: expires}, nil
}

// Valid reports whether the curve may be applied at the given time.
func (c Calibration) Valid(at time.Time) bool {
	at = at.UTC()
	return !at.Before(c.CreatedAt) && at.Before(c.ExpiresAt)
}

// Correct maps a raw scale reading to the corrected position. Readings
// outside the surveyed span take the nearest endpoint correction.
func (c Calibration) Correct(position float64) float64 {
	if len(c.Segments) == 0 {
		return position + c.Points[0].Correction()
	}
	first := c.Segments[0]
	if position <= first.Start {
		return position + first.StartCorrection
	}
	last := c.Segments[len(c.Segments)-1]
	if position >= last.End {
		return position + last.EndCorrection
	}
	for _, segment := range c.Segments {
		if segment.Contains(position) {
			return position + segment.Correction(position)
		}
	}
	return position
}

// Correction describes one corrected reading together with the curve used.
type Correction struct {
	Raw        float64   `json:"raw"`
	Corrected  float64   `json:"corrected"`
	Version    string    `json:"version"`
	Calibrated bool      `json:"calibrated"`
	Valid      bool      `json:"valid"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
}

// Apply corrects a reading and stamps it with this calibration's version.
func (c Calibration) Apply(position float64, at time.Time) Correction {
	return Correction{
		Raw:        position,
		Corrected:  c.Correct(position),
		Version:    c.Version,
		Calibrated: true,
		Valid:      c.Valid(at),
		ExpiresAt:  c.ExpiresAt,
	}
}

// Uncorrected is the passthrough result when no calibration exists yet.
func Uncorrected(position float64) Correction {
	return Correction{Raw: position, Corrected: position}
}
