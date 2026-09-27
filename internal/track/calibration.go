package track

import (
    "errors"
    "math"
    "strings"
    "time"

    "github.com/wyw14/solo-037/internal/model"
)

// ReferencePoint is one surveyed track benchmark. Nominal is the known true
// position; Forward and Reverse are the positions the carriage scale reported
// when approaching the point in each direction.
type ReferencePoint struct {
    PointID string  `json:"point_id"`
    Nominal float64 `json:"nominal"`
    Forward float64 `json:"forward"`
    Reverse float64 `json:"reverse"`
}

// Measured averages the two approach directions, cancelling scale backlash.
func (p ReferencePoint) Measured() float64 {
    return (p.Forward + p.Reverse) / 2
}

// Correction is the offset that maps a scale reading back to the nominal
// position at this point.
func (p ReferencePoint) Correction() float64 {
    return p.Nominal - p.Measured()
}

// Backlash is the forward/reverse spread observed at this point.
func (p ReferencePoint) Backlash() float64 {
    return math.Abs(p.Forward - p.Reverse)
}

// Segment is the piecewise-linear correction span between two neighbouring
// reference points, expressed in both measured and nominal coordinates.
type Segment struct {
    FromMeasured   float64 `json:"from_measured"`
    ToMeasured     float64 `json:"to_measured"`
    FromNominal    float64 `json:"from_nominal"`
    ToNominal      float64 `json:"to_nominal"`
    FromCorrection float64 `json:"from_correction"`
    ToCorrection   float64 `json:"to_correction"`
    MaxBacklash    float64 `json:"max_backlash"`
}

// Calibration is one versioned correction curve for a carriage track, valid
// until ValidUntil. Points keeps the raw survey for audit; Segments is the
// derived curve.
type Calibration struct {
    ID         model.CalibrationID `json:"id"`
    Carriage   string              `json:"carriage"`
    CreatedAt  time.Time           `json:"created_at"`
    ValidUntil time.Time           `json:"valid_until"`
    Points     []ReferencePoint    `json:"points"`
    Segments   []Segment           `json:"segments"`
}

// Build validates a survey and derives the segmented correction curve.
func Build(carriage string, points []ReferencePoint, validUntil time.Time, at time.Time) (Calibration, error) {
    if strings.TrimSpace(carriage) == "" {
        return Calibration{}, errors.New("carriage is required")
    }
    if len(points) < 2 {
        return Calibration{}, errors.New("at least two reference points are required")
    }
    if validUntil.IsZero() || !validUntil.After(at) {
        return Calibration{}, errors.New("validity must end in the future")
    }
    segments := make([]Segment, 0, len(points)-1)
    for i, point := range points {
        if strings.TrimSpace(point.PointID) == "" {
            return Calibration{}, errors.New("point id is required")
        }
        for _, value := range []float64{point.Nominal, point.Forward, point.Reverse} {
            if math.IsNaN(value) || math.IsInf(value, 0) {
                return Calibration{}, errors.New("point values must be finite")
            }
        }
        if i > 0 {
            previous := points[i-1]
            if point.Nominal <= previous.Nominal {
                return Calibration{}, errors.New("nominal positions must be strictly increasing")
            }
            if point.Measured() <= previous.Measured() {
                return Calibration{}, errors.New("measured positions must be strictly increasing")
            }
            segments = append(segments, Segment{
                FromMeasured:   previous.Measured(),
                ToMeasured:     point.Measured(),
                FromNominal:    previous.Nominal,
                ToNominal:      point.Nominal,
                FromCorrection: previous.Correction(),
                ToCorrection:   point.Correction(),
                MaxBacklash:    math.Max(previous.Backlash(), point.Backlash()),
            })
        }
    }
    return Calibration{
        ID:         model.NewCalibrationID(),
        Carriage:   carriage,
        CreatedAt:  at.UTC(),
        ValidUntil: validUntil.UTC(),
        Points:     append([]ReferencePoint(nil), points...),
        Segments:   segments,
    }, nil
}

// Expired reports whether the calibration is past its validity at the given
// time.
func (c Calibration) Expired(at time.Time) bool {
    return !at.UTC().Before(c.ValidUntil)
}

// Correct maps a raw scale reading to the corrected position using the
// segmented curve. Readings outside the surveyed range are clamped to the
// nearest endpoint correction rather than extrapolated.
func (c Calibration) Correct(raw float64) float64 {
    if len(c.Segments) == 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
        return raw
    }
    first := c.Segments[0]
    if raw <= first.FromMeasured {
        return raw + first.FromCorrection
    }
    last := c.Segments[len(c.Segments)-1]
    if raw >= last.ToMeasured {
        return raw + last.ToCorrection
    }
    for _, segment := range c.Segments {
        if raw >= segment.FromMeasured && raw <= segment.ToMeasured {
            fraction := (raw - segment.FromMeasured) / (segment.ToMeasured - segment.FromMeasured)
            return raw + segment.FromCorrection + fraction*(segment.ToCorrection-segment.FromCorrection)
        }
    }
    return raw
}
