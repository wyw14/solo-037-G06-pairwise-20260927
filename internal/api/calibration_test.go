package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-037/internal/service"
)

func TestCalibrationAndPositionHTTPFlow(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()

	validUntil := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	body := `{"carriage":"carriage-1","valid_until":"` + validUntil + `",` +
		`"points":[{"point_id":"p0","nominal":0,"forward":0.1,"reverse":-0.1},` +
		`{"point_id":"p1","nominal":10,"forward":10.2,"reverse":9.8}]}`
	created := httptest.NewRecorder()
	server.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/calibrations", strings.NewReader(body)))
	if created.Code != http.StatusOK {
		t.Fatalf("create calibration: %d %s", created.Code, created.Body)
	}
	var calibrationResponse struct {
		Data struct {
			ID      string `json:"id"`
			Expired bool   `json:"expired"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &calibrationResponse); err != nil {
		t.Fatal(err)
	}
	if calibrationResponse.Data.ID == "" || calibrationResponse.Data.Expired {
		t.Fatalf("unexpected calibration response: %s", created.Body)
	}

	listed := httptest.NewRecorder()
	server.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/calibrations", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), calibrationResponse.Data.ID) {
		t.Fatalf("list: %d %s", listed.Code, listed.Body)
	}

	run := httptest.NewRecorder()
	server.ServeHTTP(run, httptest.NewRequest(http.MethodPost, "/api/measured-runs",
		strings.NewReader(`{"name":"tow A","model_name":"hull A","section":"section-a","carriage":"carriage-1"}`)))
	var runResponse struct {
		Data service.MeasuredRun `json:"data"`
	}
	if err := json.Unmarshal(run.Body.Bytes(), &runResponse); err != nil {
		t.Fatal(err)
	}
	runID := string(runResponse.Data.Operation.ID)

	stamp := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	position := httptest.NewRecorder()
	server.ServeHTTP(position, httptest.NewRequest(http.MethodPost,
		"/api/measured-runs/"+runID+"/positions",
		strings.NewReader(`{"position":5.05,"recorded_at":"`+stamp+`"}`)))
	if position.Code != http.StatusOK {
		t.Fatalf("record position: %d %s", position.Code, position.Body)
	}
	var positionResponse struct {
		Data service.PositionOutcome `json:"data"`
	}
	if err := json.Unmarshal(position.Body.Bytes(), &positionResponse); err != nil {
		t.Fatal(err)
	}
	outcome := positionResponse.Data
	// Segment corrections: p0 measured 0 → correction 0; p1 measured 10 →
	// correction 0. Raw 5.05 sits mid-segment so corrected stays 5.05, but the
	// reading must be bound to the calibration version.
	if outcome.Raw != 5.05 || outcome.Corrected != 5.05 || outcome.CalibrationID == "" || outcome.CalibrationExpired {
		t.Fatalf("unexpected position outcome: %+v", outcome)
	}

	analysis := httptest.NewRecorder()
	server.ServeHTTP(analysis, httptest.NewRequest(http.MethodGet, "/api/measured-runs/"+runID+"/analysis", nil))
	if analysis.Code != http.StatusOK ||
		!strings.Contains(analysis.Body.String(), `"raw":5.05`) ||
		!strings.Contains(analysis.Body.String(), `"corrected":5.05`) ||
		!strings.Contains(analysis.Body.String(), `"calibration_id":"`+string(outcome.CalibrationID)+`"`) {
		t.Fatalf("analysis should expose raw, corrected and version: %d %s", analysis.Code, analysis.Body)
	}
}

func TestCalibrationHTTPRejectsInvalidSurvey(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()
	validUntil := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	body := `{"carriage":"carriage-1","valid_until":"` + validUntil + `",` +
		`"points":[{"point_id":"p0","nominal":0,"forward":0.1,"reverse":-0.1}]}`
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/calibrations", strings.NewReader(body)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("single-point survey should be rejected: %d %s", response.Code, response.Body)
	}
	missing := httptest.NewRecorder()
	server.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/calibrations/nope", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown calibration should 404: %d", missing.Code)
	}
}
