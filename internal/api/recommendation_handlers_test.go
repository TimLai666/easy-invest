package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func TestCreateRecommendationRunRequestStrictJSON(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/api/v1/recommendations/runs", strings.NewReader(`{"unknown":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var body createRecommendationRunRequest
	if err := decodeOptionalJSON(req, &body); err == nil {
		t.Fatal("decodeOptionalJSON 應拒絕未知欄位")
	}
}

func TestCreateRecommendationRunRequestOverrideParsesDecimalWeights(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/api/v1/recommendations/runs", strings.NewReader(`{"target_weights":{"0050":"0.4","2330":0.6}}`))
	if err != nil {
		t.Fatal(err)
	}
	var body createRecommendationRunRequest
	if err := decodeOptionalJSON(req, &body); err != nil {
		t.Fatalf("decodeOptionalJSON() error = %v", err)
	}
	override, detail, err := body.override()
	if err != nil {
		t.Fatalf("override() error = %v, detail = %+v", err, detail)
	}
	if got := override.TargetWeights["0050"]; !got.Equal(decimal.RequireFromString("0.4")) {
		t.Fatalf("0050 weight = %s, want 0.4", got)
	}
	if got := override.TargetWeights["2330"]; !got.Equal(decimal.RequireFromString("0.6")) {
		t.Fatalf("2330 weight = %s, want 0.6", got)
	}
}

func TestCreateRecommendationRunRequestOverrideRejectsOutOfRangeWeights(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/api/v1/recommendations/runs", strings.NewReader(`{"target_weights":{"0050":"1.2"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var body createRecommendationRunRequest
	if err := decodeOptionalJSON(req, &body); err != nil {
		t.Fatalf("decodeOptionalJSON() error = %v", err)
	}
	_, detail, err := body.override()
	if err == nil {
		t.Fatal("override() 應拒絕超出範圍的權重")
	}
	if detail.Field != "target_weights.0050" {
		t.Fatalf("detail field = %q, want target_weights.0050", detail.Field)
	}
}
