package account

import (
	"encoding/json"
	"testing"
)

func TestParsePlanAcrossAPIShapes(t *testing.T) {
	tests := []struct {
		name string
		org  orgResponse
		want string
	}{
		{"settings tier", orgResponse{Settings: struct {
			Tier string `json:"tier"`
		}{Tier: "pro"}}, "Pro"},
		{"billing tier max20x", orgResponse{BillingInfo: struct {
			Tier string `json:"tier"`
			Plan string `json:"plan"`
		}{Tier: "max_20x"}}, "Max 20x"},
		{"capabilities bool", orgResponse{Capabilities: json.RawMessage(`{"claude_pro":false,"claude_max":true}`)}, "Max"},
		{"capabilities list priority", orgResponse{Capabilities: json.RawMessage(`["claude_pro","claude_team"]`)}, "Team"},
		{"unknown tier passthrough", orgResponse{Settings: struct {
			Tier string `json:"tier"`
		}{Tier: "custom"}}, "custom"},
		{"empty", orgResponse{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePlan(tt.org); got != tt.want {
				t.Fatalf("parsePlan = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizePlanCanonicalizes(t *testing.T) {
	cases := map[string]string{
		"free":       "Free",
		"pro":        "Pro",
		"max_5x":     "Max",
		"max_20x":    "Max 20x",
		"team":       "Team",
		"enterprise": "Enterprise",
		"mystery":    "mystery",
	}
	for in, want := range cases {
		if got := normalizePlan(in); got != want {
			t.Fatalf("normalizePlan(%q) = %q, want %q", in, got, want)
		}
	}
}
