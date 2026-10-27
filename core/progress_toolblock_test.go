package core

import (
	"strings"
	"testing"
	"time"
)

func intPtr(i int) *int { return &i }

func TestToolBlockProgress_MergesInputStatusOutput(t *testing.T) {
	tb := newToolBlockProgress(LangChinese)

	got := tb.append(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "Bash", Text: "git status"}, "🔧 fallback")
	want := "```#1·Bash\n$ git status\n──────── ⏳ … ────────\n```"
	if got != want {
		t.Fatalf("running block =\n%s\nwant\n%s", got, want)
	}

	got = tb.append(ProgressCardEntry{Kind: ProgressEntryToolResult, Text: "clean", Success: boolPtr(true), ExitCode: intPtr(0)}, "🧾 fallback")
	want = "```#1·Bash\n$ git status\n──────── 🟢 ok · 退出码 0 ────────\nclean\n```"
	if got != want {
		t.Fatalf("finished block =\n%s\nwant\n%s", got, want)
	}
}

func TestToolBlockProgress_PairsResultsInOrder(t *testing.T) {
	tb := newToolBlockProgress(LangEnglish)
	tb.append(ProgressCardEntry{Kind: ProgressEntryThinking, Text: "plan"}, "💭 plan")
	tb.append(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "Read", Text: "a.go"}, "f")
	tb.append(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "Bash", Text: "false"}, "f")
	tb.append(ProgressCardEntry{Kind: ProgressEntryToolResult, Text: "package a", Success: boolPtr(true)}, "r1")
	got := tb.append(ProgressCardEntry{Kind: ProgressEntryToolResult, Success: boolPtr(false), ExitCode: intPtr(1)}, "r2")

	want := strings.Join([]string{
		"💭 plan",
		"```#1·Read\n> a.go\n──────── 🟢 ok ────────\npackage a\n```",
		"```#2·Bash\n$ false\n──────── 🔴 failed · Exit 1 ────────\n```",
	}, "\n\n")
	if got != want {
		t.Fatalf("progress =\n%s\nwant\n%s", got, want)
	}
}

func TestToolBlockProgress_UnmatchedResultUsesFallback(t *testing.T) {
	tb := newToolBlockProgress(LangEnglish)
	got := tb.append(ProgressCardEntry{Kind: ProgressEntryToolResult, Text: "orphan"}, "🧾 orphan")
	if got != "🧾 orphan" {
		t.Fatalf("got %q, want fallback", got)
	}
}

func TestToolBlockProgress_LengthensFenceAroundBackticks(t *testing.T) {
	tb := newToolBlockProgress(LangEnglish)
	got := tb.append(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "Write", Text: "```go\nx\n```"}, "f")
	if !strings.HasPrefix(got, "````#1·Write\n") || !strings.HasSuffix(got, "\n````") {
		t.Fatalf("fence not lengthened: %q", got)
	}
}

type stubToolBlockPlatform struct {
	stubCompactProgressPlatform
}

func (p *stubToolBlockPlatform) ToolBlockProgress() bool { return true }

func TestProcessInteractiveEvents_ToolBlockProgressTruncatesInputAndOutput(t *testing.T) {
	p := &stubToolBlockPlatform{stubCompactProgressPlatform{stubPlatformEngine: stubPlatformEngine{n: "telegram"}, style: "compact"}}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	e.SetDisplayConfig(DisplayCfg{ThinkingMessages: true, ThinkingMaxLen: 300, ToolMaxLen: 20, ToolMessages: true, Mode: "full"})

	sessionKey := "telegram:user-tool-block"
	session := e.sessions.GetOrCreateActive(sessionKey)
	agentSession := newControllableSession("s-tool-block")
	state := &interactiveState{agentSession: agentSession, platform: p, replyCtx: "ctx-tool-block"}
	e.interactiveStates[sessionKey] = state

	agentSession.events <- Event{Type: EventToolUse, ToolName: "Bash", ToolInput: strings.Repeat("i", 50)}
	agentSession.events <- Event{Type: EventToolResult, ToolResult: strings.Repeat("o", 50), ToolSuccess: boolPtr(true), ToolExitCode: intPtr(0)}
	agentSession.events <- Event{Type: EventResult, Content: "done", Done: true}
	e.processInteractiveEvents(state, session, e.sessions, sessionKey, "m-tool-block", time.Now(), nil, nil, state.replyCtx, 0)

	edits := p.getPreviewEdits()
	if len(edits) == 0 {
		t.Fatalf("no progress edits; starts=%v", p.getPreviewStarts())
	}
	want := "```#1·Bash\n$ " + strings.Repeat("i", 20) + "...\n──────── 🟢 ok · Exit 0 ────────\n" + strings.Repeat("o", 20) + "...\n```"
	if got := edits[len(edits)-1]; got != want {
		t.Fatalf("progress =\n%s\nwant\n%s", got, want)
	}
}
