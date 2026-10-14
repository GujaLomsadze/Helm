package model

import "strings"

// SessionKey preserves existing integrations' IDs; Codex IDs have their own namespace.
func SessionKey(provider, native string) string {
	if provider == ProviderCodex && !strings.HasPrefix(native, "codex:") {
		return "codex:" + native
	}
	return native
}

func NativeSessionID(provider, key string) string {
	if provider == ProviderCodex {
		return strings.TrimPrefix(key, "codex:")
	}
	return key
}
