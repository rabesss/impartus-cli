package tui

import "testing"

func TestCommandRegistryHasStableUniqueIDsAndRequiredDirectKeys(t *testing.T) {
	t.Parallel()
	seenIDs := make(map[commandID]struct{}, len(commands))
	seenKeys := make(map[string]struct{})
	for _, candidate := range commands {
		if candidate.id == "" || candidate.label == "" || candidate.context == nil || candidate.action == nil {
			t.Fatalf("incomplete command: %+v", candidate)
		}
		if _, duplicate := seenIDs[candidate.id]; duplicate {
			t.Fatalf("duplicate command ID %q", candidate.id)
		}
		seenIDs[candidate.id] = struct{}{}
		for _, key := range candidate.keys {
			seenKeys[key] = struct{}{}
		}
	}
	for _, required := range []string{"enter", "d", "l", "/", "r", "!", "i", "esc", "q", "ctrl+c", "ctrl+p", "?", "tab", "shift+tab"} {
		if _, found := seenKeys[required]; !found {
			t.Errorf("direct key %q is absent from the command registry", required)
		}
	}
}

func TestCommandSurfacesUseRegistryEntries(t *testing.T) {
	t.Parallel()
	for _, candidate := range commands {
		if !candidate.palette && !candidate.footer && !candidate.inspector {
			continue
		}
		if candidate.hint == "" {
			t.Errorf("surfaced command %q has no registry hint", candidate.id)
		}
	}
}
