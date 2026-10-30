package owlcms

import (
	"strings"
	"testing"
)

func tokenIn(t *testing.T, env []string) string {
	t.Helper()
	found := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, controlPanelTokenEnv+"="); ok {
			if found != "" {
				t.Fatalf("token set twice: %v", env)
			}
			found = value
		}
	}
	return found
}

func TestEachLaunchGetsANewToken(t *testing.T) {
	base := []string{"PATH=/bin", "OWLCMS_PORT=8080"}

	first, firstToken := withNewControlPanelToken(base)
	second, secondToken := withNewControlPanelToken(base)

	if len(firstToken) != 12 || len(secondToken) != 12 {
		t.Fatalf("tokens must have 12 characters: %q %q", firstToken, secondToken)
	}
	if firstToken == secondToken {
		t.Fatal("a restart must not reuse the previous token")
	}
	if tokenIn(t, first) != firstToken || tokenIn(t, second) != secondToken {
		t.Fatal("the environment must carry the token returned")
	}
	if lastLaunchToken != secondToken {
		t.Fatal("the last launch token must be remembered for stop and copy")
	}
	if len(base) != 2 || tokenIn(t, base) != "" {
		t.Fatalf("the launch parameters must not be modified: %v", base)
	}
}

func TestTheControlPanelTokenReplacesAnyOtherValue(t *testing.T) {
	base := []string{controlPanelTokenEnv + "=set-by-hand", "OWLCMS_PORT=8080"}

	env, token := withNewControlPanelToken(base)

	if tokenIn(t, env) != token || token == "set-by-hand" {
		t.Fatalf("the generated token must win: %v", env)
	}
	if base[0] != controlPanelTokenEnv+"=set-by-hand" {
		t.Fatalf("the launch parameters must not be modified: %v", base)
	}
}
