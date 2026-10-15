package claude

import (
	"testing"

	"github.com/GujaLomsadze/trk/internal/agent"
	"github.com/GujaLomsadze/trk/internal/claudecfg"
	"github.com/GujaLomsadze/trk/internal/model"
)

func TestKindForHook(t *testing.T) {
	cases := map[string]string{
		"PreToolUse": model.KindToolPre, "PostToolUseFailure": model.KindToolFail,
		"PermissionRequest": model.KindPermission, "Stop": model.KindStop, "Whatever": model.KindHookOther, "": model.KindHookOther,
	}
	for in, want := range cases {
		if got := KindForHook(in); got != want {
			t.Errorf("KindForHook(%q) = %q, want %q", in, got, want)
		}
	}
	for _, ev := range claudecfg.HookEvents {
		if KindForHook(ev) == model.KindHookOther {
			t.Errorf("registered hook %q has no kind", ev)
		}
	}
}

func TestNormalizeKeepsPayload(t *testing.T) {
	raw := []byte(`{"session_id":"s1","hook_event_name":"PreToolUse","tool_name":"Bash"}`)
	n, ok := Adapter{}.Normalize(raw)
	if !ok || n.Kind != model.KindToolPre || n.Native != "s1" || string(n.Payload) != string(raw) || n.Key != "" {
		t.Fatalf("%+v %v", n, ok)
	}
}

func TestLaunch(t *testing.T) {
	l, _ := Adapter{}.Launch(agentSpec("abc", true, " hi "))
	if l.Exe != "claude" || len(l.Args) != 4 || l.Args[0] != "--resume" || l.Args[2] != "--fork-session" || l.Args[3] != "hi" {
		t.Fatal(l)
	}
}

func agentSpec(resume string, fork bool, prompt string) agent.LaunchSpec {
	return agent.LaunchSpec{Resume: resume, Fork: fork, Prompt: prompt}
}
