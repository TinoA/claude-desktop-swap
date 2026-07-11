package account

import (
	"encoding/json"
	"strings"
)

// parsePlan extracts the subscription plan from an org response, trying
// multiple field shapes since the Claude.ai API has varied over time.
func parsePlan(org orgResponse) string {
	if org.Settings.Tier != "" {
		return normalizePlan(org.Settings.Tier)
	}
	if org.BillingInfo.Tier != "" {
		return normalizePlan(org.BillingInfo.Tier)
	}
	if org.BillingInfo.Plan != "" {
		return normalizePlan(org.BillingInfo.Plan)
	}
	// capabilities as {"claude_pro": true, ...}
	var capsBool map[string]bool
	if json.Unmarshal(org.Capabilities, &capsBool) == nil {
		if p := planFromCapsBool(capsBool); p != "" {
			return p
		}
	}
	// capabilities as ["claude_pro", ...]
	var capsList []string
	if json.Unmarshal(org.Capabilities, &capsList) == nil {
		return planFromCapsList(capsList)
	}
	return ""
}

func normalizePlan(tier string) string {
	switch strings.ToLower(strings.ReplaceAll(tier, "_", "")) {
	case "free":
		return "Free"
	case "pro":
		return "Pro"
	case "max", "max1x", "max5x":
		return "Max"
	case "max20x":
		return "Max 20x"
	case "prolite":
		return "Pro Lite"
	case "team":
		return "Team"
	case "enterprise":
		return "Enterprise"
	case "education", "edu":
		return "Edu"
	default:
		return tier
	}
}

func planFromCapsBool(caps map[string]bool) string {
	switch {
	case caps["claude_max"]:
		return "Max"
	case caps["claude_pro"]:
		return "Pro"
	case caps["claude_team"]:
		return "Team"
	case caps["claude_enterprise"]:
		return "Enterprise"
	default:
		return ""
	}
}

func planFromCapsList(caps []string) string {
	priority := map[string]int{
		"claude_max": 4, "claude_enterprise": 3,
		"claude_team": 2, "claude_pro": 1,
	}
	names := map[string]string{
		"claude_max": "Max", "claude_enterprise": "Enterprise",
		"claude_team": "Team", "claude_pro": "Pro",
	}
	best, bestP := "", 0
	for _, c := range caps {
		if p := priority[c]; p > bestP {
			best, bestP = names[c], p
		}
	}
	return best
}
