package profile

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointCapturesOnlyAccountScopedConfiguration(t *testing.T) {
	store := newTestStore(t)
	appData := syntheticAppData(t, "saved")
	writeAccountFixture(t, appData, "token-a", "token-v2-a", "account-a", "owner-a", "dark", true)

	if err := store.Checkpoint("work", appData); err != nil {
		t.Fatal(err)
	}
	state, err := readAccountState(filepath.Join(store.profileDir("work"), accountStateFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Config) != len(accountConfigKeys) {
		t.Fatalf("saved config keys = %d", len(state.Config))
	}
	for _, key := range accountConfigKeys {
		if _, ok := state.Config[key]; !ok {
			t.Fatalf("missing account key %q", key)
		}
	}
	if len(state.CoworkOwner) == 0 {
		t.Fatal("Cowork owner was not captured")
	}
	data, err := os.ReadFile(filepath.Join(store.profileDir("work"), accountStateFile))
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid(data) == false || containsJSONKey(data, "theme") || containsJSONKey(data, "keepGlobal") {
		t.Fatal("global configuration leaked into account state")
	}
}

func TestRestoreMergesAccountConfigurationWithoutReplacingGlobals(t *testing.T) {
	store := newTestStore(t)
	saved := syntheticAppData(t, "saved")
	writeAccountFixture(t, saved, "token-a", "token-v2-a", "account-a", "owner-a", "saved-theme", true)
	if err := store.Checkpoint("work", saved); err != nil {
		t.Fatal(err)
	}

	live := syntheticAppData(t, "live")
	writeAccountFixture(t, live, "token-b", "token-v2-b", "account-b", "owner-b", "live-theme", true)
	if err := store.Restore("work", live); err != nil {
		t.Fatal(err)
	}

	config := readFixtureObject(t, filepath.Join(live, claudeConfigFile))
	assertJSONString(t, config, "oauth:tokenCache", encryptedFixture("token-a"))
	assertJSONString(t, config, "oauth:tokenCacheV2", encryptedFixture("token-v2-a"))
	assertJSONString(t, config, "lastKnownAccountUuid", "account-a")
	assertJSONString(t, config, "theme", "live-theme")
	cowork := readFixtureObject(t, filepath.Join(live, coworkOpsFile))
	assertJSONString(t, cowork, coworkOwnerKey, "owner-a")
	if _, ok := cowork["keepGlobal"]; !ok {
		t.Fatal("Cowork global setting was removed")
	}
}

func TestWipeRemovesAccountConfigurationAndPreservesGlobals(t *testing.T) {
	store := newTestStore(t)
	appData := syntheticAppData(t, "live")
	writeAccountFixture(t, appData, "token-a", "token-v2-a", "account-a", "owner-a", "dark", true)

	if err := store.Wipe(appData); err != nil {
		t.Fatal(err)
	}
	config := readFixtureObject(t, filepath.Join(appData, claudeConfigFile))
	for _, key := range accountConfigKeys {
		if _, ok := config[key]; ok {
			t.Fatalf("account key %q remains after wipe", key)
		}
	}
	assertJSONString(t, config, "theme", "dark")
	cowork := readFixtureObject(t, filepath.Join(appData, coworkOpsFile))
	if _, ok := cowork[coworkOwnerKey]; ok {
		t.Fatal("Cowork owner remains after wipe")
	}
	if _, ok := cowork["keepGlobal"]; !ok {
		t.Fatal("Cowork global setting was removed")
	}
}

func TestHasPersistedAccountStateRequiresEncryptedAuthAndIdentity(t *testing.T) {
	appData := t.TempDir()
	writeAccountFixture(t, appData, "token-a", "token-v2-a", "account-a", "owner-a", "dark", true)
	if !HasPersistedAccountStateAt(appData) {
		t.Fatal("complete encrypted account state was not detected")
	}

	missingIdentity := t.TempDir()
	writeFixtureObject(t, filepath.Join(missingIdentity, claudeConfigFile), map[string]any{
		"oauth:tokenCache": encryptedFixture("token-a"),
	})
	if HasPersistedAccountStateAt(missingIdentity) {
		t.Fatal("authentication state without account identity was accepted")
	}

	plaintext := t.TempDir()
	writeFixtureObject(t, filepath.Join(plaintext, claudeConfigFile), map[string]any{
		"oauth:tokenCache":     "plain-token",
		"lastKnownAccountUuid": "account-a",
	})
	if HasPersistedAccountStateAt(plaintext) {
		t.Fatal("plaintext authentication state was accepted")
	}
}

func TestPersistedAccountStateFingerprintChangesWithEncryptedState(t *testing.T) {
	appData := t.TempDir()
	writeAccountFixture(t, appData, "token-a", "token-v2-a", "account-a", "owner-a", "dark", true)
	first := PersistedAccountStateFingerprintAt(appData)
	if first == "" {
		t.Fatal("complete account state has no fingerprint")
	}

	writeAccountFixture(t, appData, "token-b", "token-v2-b", "account-a", "owner-a", "light", true)
	second := PersistedAccountStateFingerprintAt(appData)
	if second == "" || second == first {
		t.Fatal("encrypted account state change was not detected")
	}
}

func TestInspectRejectsTamperedAccountState(t *testing.T) {
	store := newTestStore(t)
	appData := syntheticAppData(t, "saved")
	writeAccountFixture(t, appData, "token-a", "token-v2-a", "account-a", "owner-a", "dark", true)
	if err := store.Checkpoint("work", appData); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.profileDir("work"), accountStateFile), []byte(`{"version":1}`), filePerm); err != nil {
		t.Fatal(err)
	}
	if inspection := store.Inspect("work"); inspection.Health != HealthUnknown {
		t.Fatalf("tampered account state health = %s", inspection.Health)
	}
}

func TestCheckpointRejectsPlaintextOAuthCache(t *testing.T) {
	store := newTestStore(t)
	appData := syntheticAppData(t, "saved")
	writeFixtureObject(t, filepath.Join(appData, claudeConfigFile), map[string]any{
		"oauth:tokenCache": "plain-token",
	})
	if err := store.Checkpoint("work", appData); err == nil {
		t.Fatal("plaintext OAuth cache was saved")
	}
}

func writeAccountFixture(t *testing.T, appData, token, tokenV2, account, owner, theme string, keepGlobal bool) {
	t.Helper()
	config := map[string]any{
		"oauth:tokenCache":     encryptedFixture(token),
		"oauth:tokenCacheV2":   encryptedFixture(tokenV2),
		"lastKnownAccountUuid": account,
		"theme":                theme,
	}
	cowork := map[string]any{coworkOwnerKey: owner}
	if keepGlobal {
		cowork["keepGlobal"] = true
	}
	writeFixtureObject(t, filepath.Join(appData, claudeConfigFile), config)
	writeFixtureObject(t, filepath.Join(appData, coworkOpsFile), cowork)
}

func encryptedFixture(value string) string {
	return base64.StdEncoding.EncodeToString([]byte("v10" + value))
}

func writeFixtureObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, path, string(data))
}

func readFixtureObject(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	value, _, err := readJSONObject(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertJSONString(t *testing.T, value map[string]json.RawMessage, key, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(value[key], &got); err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func containsJSONKey(data []byte, key string) bool {
	var value map[string]json.RawMessage
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	_, ok := value[key]
	return ok
}
