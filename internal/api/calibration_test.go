package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-037/internal/service"
	"github.com/wyw14/solo-037/internal/track"
)

func TestTrackCalibrationHTTPFlow(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()
	stamp := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	point := `{"label":"A","nominal":10,"forward":10.2,"reverse":10.0,"recorded_by":"operator-1","recorded_at":"` + stamp + `"}`
	recorded := httptest.NewRecorder()
	server.ServeHTTP(recorded, httptest.NewRequest(http.MethodPost, "/api/track-calibration/reference-points", strings.NewReader(point)))
	if recorded.Code != http.StatusOK {
		t.Fatalf("record point: %d %s", recorded.Code, recorded.Body)
	}
	built := httptest.NewRecorder()
	server.ServeHTTP(built, httptest.NewRequest(http.MethodPost, "/api/track-calibration/calibrations", strings.NewReader(`{"valid_hours":24}`)))
	if built.Code != http.StatusOK {
		t.Fatalf("build: %d %s", built.Code, built.Body)
	}
	var buildResponse struct {
		Data track.Calibration `json:"data"`
	}
	if err := json.Unmarshal(built.Body.Bytes(), &buildResponse); err != nil {
		t.Fatal(err)
	}
	if buildResponse.Data.Version == "" || !buildResponse.Data.ExpiresAt.After(buildResponse.Data.CreatedAt) {
		t.Fatalf("calibration missing version or expiry: %+v", buildResponse.Data)
	}
	corrected := httptest.NewRecorder()
	server.ServeHTTP(corrected, httptest.NewRequest(http.MethodPost, "/api/track-calibration/correct", strings.NewReader(`{"position":12.3}`)))
	if corrected.Code != http.StatusOK {
		t.Fatalf("correct: %d %s", corrected.Code, corrected.Body)
	}
	var correctResponse struct {
		Data track.Correction `json:"data"`
	}
	if err := json.Unmarshal(corrected.Body.Bytes(), &correctResponse); err != nil {
		t.Fatal(err)
	}
	got := correctResponse.Data
	if got.Raw != 12.3 || math.Abs(got.Corrected-12.2) > 1e-9 || !got.Calibrated || !got.Valid || got.Version != buildResponse.Data.Version {
		t.Fatalf("correction did not expose raw, corrected and version: %+v", got)
	}
}

func TestTrackCalibrationRejectsBadInput(t *testing.T) {
	runtime, err := service.OpenRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewRouter(runtime).Handler()
	missingOperator := `{"nominal":10,"forward":10.1,"reverse":9.9,"recorded_at":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/track-calibration/reference-points", strings.NewReader(missingOperator)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing operator accepted: %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/track-calibration/calibrations", strings.NewReader(`{"valid_hours":1}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("calibration without reference points accepted: %d", recorder.Code)
	}
}
