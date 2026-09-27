package api

import (
	"net/http"
	"time"

	"github.com/wyw14/solo-037/internal/service"
	"github.com/wyw14/solo-037/internal/track"
)

type CalibrationService interface {
	RecordReferencePoint(service.ReferenceInput) (track.ReferencePoint, error)
	ReferencePoints() []track.ReferencePoint
	BuildTrackCalibration(validHours float64) (track.Calibration, error)
	TrackCalibrations() []track.Calibration
	CorrectTrackPosition(position float64, at time.Time) (track.Correction, error)
}

type buildCalibrationRequest struct {
	ValidHours float64 `json:"valid_hours"`
}

type correctPositionRequest struct {
	Position float64   `json:"position"`
	At       time.Time `json:"at"`
}

func (r *Router) referencePoints(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, r.dependencies.ReferencePoints())
}

func (r *Router) recordReferencePoint(w http.ResponseWriter, request *http.Request) {
	var input service.ReferenceInput
	if err := decodeMeasurementBody(request, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	point, err := r.dependencies.RecordReferencePoint(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, point)
}

func (r *Router) calibrations(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, r.dependencies.TrackCalibrations())
}

func (r *Router) buildCalibration(w http.ResponseWriter, request *http.Request) {
	var input buildCalibrationRequest
	if err := decodeMeasurementBody(request, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	curve, err := r.dependencies.BuildTrackCalibration(input.ValidHours)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, curve)
}

func (r *Router) correctPosition(w http.ResponseWriter, request *http.Request) {
	var input correctPositionRequest
	if err := decodeMeasurementBody(request, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	at := input.At
	if at.IsZero() {
		at = time.Now()
	}
	correction, err := r.dependencies.CorrectTrackPosition(input.Position, at)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, correction)
}
