package account

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	orgsEndpoint    = "https://claude.ai/api/organizations"
	accountEndpoint = "https://claude.ai/api/account"
	userAgent       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var defaultClient = &http.Client{Timeout: 10 * time.Second}

// orgResponse covers the known shapes of the /api/organizations response.
type orgResponse struct {
	Email        string          `json:"email"`
	Capabilities json.RawMessage `json:"capabilities"`
	Settings     struct {
		Tier string `json:"tier"`
	} `json:"settings"`
	BillingInfo struct {
		Tier string `json:"tier"`
		Plan string `json:"plan"`
	} `json:"billing_info"`
}

type accountResponse struct {
	Email string `json:"email"`
}

// fetchFromAPI calls the Claude.ai API using a decrypted sessionKey cookie and
// returns the best-effort email and plan for the account.
func fetchFromAPI(sessionKey string) (Info, error) {
	info := Info{}

	orgs, err := getJSON[[]orgResponse](orgsEndpoint, sessionKey)
	if err == nil && len(orgs) > 0 {
		info.Plan = parsePlan(orgs[0])
		info.Email = orgs[0].Email
	}

	// Some API versions omit email from the org response — try the account endpoint.
	if info.Email == "" {
		acc, err := getJSON[accountResponse](accountEndpoint, sessionKey)
		if err == nil {
			info.Email = acc.Email
		}
	}

	return info, nil
}

// getJSON performs an authenticated GET request and unmarshals the JSON body into T.
func getJSON[T any](url, sessionKey string) (T, error) {
	var zero T
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Cookie", "sessionKey="+sessionKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://claude.ai/")

	resp, err := defaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}
	var result T
	return result, json.Unmarshal(body, &result)
}
