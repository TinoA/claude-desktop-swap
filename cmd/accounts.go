package cmd

import (
	"github.com/FranCalveyra/claude-desktop-swap/internal/account"
	"github.com/FranCalveyra/claude-desktop-swap/internal/profile"
)

// enrichLiveAccounts overlays live Claude.ai account info (email, plan) onto the
// given profiles, keeping any cached values when a live lookup comes back empty
// (offline, expired session, or unsupported platform).
func enrichLiveAccounts(store *profile.Store, profiles []profile.Meta) {
	if len(profiles) == 0 {
		return
	}
	paths := make(map[string]string, len(profiles))
	for _, p := range profiles {
		paths[p.Name] = store.ProfileCookiesPath(p.Name)
	}
	infos := account.FetchMany(paths)
	for i := range profiles {
		info := infos[profiles[i].Name]
		if info.Email != "" {
			profiles[i].Email = info.Email
		}
		if info.Plan != "" {
			profiles[i].Plan = info.Plan
		}
	}
}
