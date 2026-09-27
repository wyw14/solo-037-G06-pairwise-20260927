package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-037/internal/model"
	"github.com/wyw14/solo-037/internal/service"
	"github.com/wyw14/solo-037/internal/track"
)

type CalibrationService interface {
	RecordCalibration(carriage string, points []track.ReferencePoint, validUntil time.Time) (track.Calibration, error)
	ListCalibrations() []track.Calibration
	Calibration(model.CalibrationID) (track.Calibration, bool)
	RecordPosition(model.RunID, service.PositionInput) (service.PositionOutcome, error)
}

type createCalibrationRequest struct {
	Carriage   string                 `json:"carriage"`
	ValidUntil time.Time              `json:"valid_until"`
	Points     []track.ReferencePoint `json:"points"`
}

// calibrationView adds the request-time expiry flag to a stored version.
type calibrationView struct {
	track.Calibration
	Expired bool `json:"expired"`
}

func viewCalibration(calibration track.Calibration) calibrationView {
	return calibrationView{Calibration: calibration, Expired: calibration.Expired(time.Now().UTC())}
}

func (r *Router) createCalibration(w http.ResponseWriter, request *http.Request) {
	var input createCalibrationRequest
	if err := decodeMeasurementBody(request, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	value, err := r.dependencies.RecordCalibration(input.Carriage, input.Points, input.ValidUntil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, viewCalibration(value))
}

func (r *Router) calibrations(w http.ResponseWriter, _ *http.Request) {
	versions := r.dependencies.ListCalibrations()
	result := make([]calibrationView, 0, len(versions))
	for _, version := range versions {
		result = append(result, viewCalibration(version))
	}
	writeJSON(w, result)
}

func (r *Router) calibration(w http.ResponseWriter, request *http.Request) {
	value, ok := r.dependencies.Calibration(model.CalibrationID(chi.URLParam(request, "id")))
	if !ok {
		http.Error(w, "calibration not found", http.StatusNotFound)
		return
	}
	writeJSON(w, viewCalibration(value))
}

func (r *Router) recordPosition(w http.ResponseWriter, request *http.Request) {
	var input service.PositionInput
	if err := decodeMeasurementBody(request, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	value, err := r.dependencies.RecordPosition(model.RunID(chi.URLParam(request, "id")), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, value)
}
