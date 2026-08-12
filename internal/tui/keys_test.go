package tui

import (
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestRetryBindingIsDiscoverable(t *testing.T) {
	t.Parallel()

	keys := newKeyMap()
	if got := keys.Retry.Keys(); len(got) != 1 || got[0] != "r" {
		t.Fatalf("Retry.Keys() = %v, want [r]", got)
	}
	if got := keys.Retry.Help(); got.Key != "r" || got.Desc != "retry" {
		t.Fatalf("Retry.Help() = %+v, want r/retry", got)
	}
	if !containsBinding(keys.ShortHelp(), "r") {
		t.Fatal("ShortHelp() does not include retry")
	}
	var full []string
	for _, group := range keys.FullHelp() {
		for _, binding := range group {
			full = append(full, binding.Keys()...)
		}
	}
	if !containsKey(full, "r") {
		t.Fatal("FullHelp() does not include retry")
	}
}

func containsBinding(bindings []key.Binding, want string) bool {
	for _, binding := range bindings {
		if containsKey(binding.Keys(), want) {
			return true
		}
	}
	return false
}

func containsKey(keys []string, want string) bool {
	for _, candidate := range keys {
		if candidate == want {
			return true
		}
	}
	return false
}
