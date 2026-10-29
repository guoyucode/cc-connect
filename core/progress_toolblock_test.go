package core

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func intPtr(i int) *int { return &i }

func TestToolBlockProgress_MergesInputStatusOutput(t *testing.T) {
	tb := newToolBlockProgress(LangChinese, 0)

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
	tb := newToolBlockProgress(LangEnglish, 0)
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
	tb := newToolBlockProgress(LangEnglish, 0)
	got := tb.append(ProgressCardEntry{Kind: ProgressEntryToolResult, Text: "orphan"}, "🧾 orphan")
	if got != "🧾 orphan" {
		t.Fatalf("got %q, want fallback", got)
	}
}

func TestToolBlockProgress_LengthensFenceAroundBackticks(t *testing.T) {
	tb := newToolBlockProgress(LangEnglish, 0)
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

func TestToolBlockProgress_SegmentContinuesNumbering(t *testing.T) {
	w := &compactProgressWriter{toolBlocks: newToolBlockProgress(LangEnglish, 0)}
	tb := w.toolBlocks
	tb.append(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "Bash", Text: "ls"}, "f")
	w.MarkSegment()
	if !tb.startSegment() {
		t.Fatal("startSegment = false after MarkSegment")
	}
	got := tb.append(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "Read", Text: "a.go"}, "f")
	if want := "```#2·Read\n> a.go\n──────── ⏳ … ────────\n```"; got != want {
		t.Fatalf("new segment =\n%s\nwant\n%s", got, want)
	}
	if tb.startSegment() {
		t.Fatal("startSegment = true without a new MarkSegment")
	}
}

func TestToolBlockProgress_MarkSegmentIgnoredBeforeAnyEntry(t *testing.T) {
	w := &compactProgressWriter{toolBlocks: newToolBlockProgress(LangEnglish, 0)}
	w.MarkSegment()
	if w.toolBlocks.startSegment() {
		t.Fatal("text before the first entry must not split")
	}
	(&compactProgressWriter{}).MarkSegment() // no-op without tool blocks
}

func TestToolBlockProgress_TrimsWholeBlocks(t *testing.T) {
	block := func(n int) string {
		return "```#" + itoa(n) + "·Bash\n$ " + strings.Repeat("c", 40) + "\n──────── ⏳ … ────────\n```"
	}
	tb := newToolBlockProgress(LangChinese, 2*len([]rune(block(1)))+40)
	for i := 0; i < 4; i++ {
		tb.append(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "Bash", Text: strings.Repeat("c", 40)}, "f")
	}
	got := tb.append(ProgressCardEntry{Kind: ProgressEntryToolResult, Text: "done", Success: boolPtr(true)}, "r")

	if !strings.HasPrefix(got, "…（前面 2 条已省略）\n\n```#3·Bash\n") {
		t.Fatalf("trimmed progress does not start at a whole block:\n%s", got)
	}
	if n := strings.Count("\n"+got, "\n```"); n%2 != 0 {
		t.Fatalf("unbalanced fences (%d):\n%s", n, got)
	}
	// The oldest pending call (#3, as #1 and #2 were dropped) gets the result.
	if !strings.Contains(got, "```#3·Bash\n$ "+strings.Repeat("c", 40)+"\n──────── 🟢 ok ────────\ndone\n```") {
		t.Fatalf("result not merged into #3:\n%s", got)
	}
	if utf8.RuneCountInString(got) > tb.maxRunes {
		t.Fatalf("len %d > max %d", utf8.RuneCountInString(got), tb.maxRunes)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestProcessInteractiveEvents_ToolBlockProgressSplitsAfterText(t *testing.T) {
	p := &stubToolBlockPlatform{stubCompactProgressPlatform{stubPlatformEngine: stubPlatformEngine{n: "telegram"}, style: "compact"}}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	e.SetDisplayConfig(DisplayCfg{ThinkingMessages: true, ThinkingMaxLen: 300, ToolMaxLen: 300, ToolMessages: true, Mode: "full"})

	sessionKey := "telegram:user-tool-split"
	session := e.sessions.GetOrCreateActive(sessionKey)
	agentSession := newControllableSession("s-tool-split")
	state := &interactiveState{agentSession: agentSession, platform: p, replyCtx: "ctx-tool-split"}
	e.interactiveStates[sessionKey] = state

	agentSession.events <- Event{Type: EventToolUse, ToolName: "Bash", ToolInput: "ls"}
	agentSession.events <- Event{Type: EventToolResult, ToolResult: "a.go", ToolSuccess: boolPtr(true)}
	agentSession.events <- Event{Type: EventText, Content: "Found one file, reading it."}
	agentSession.events <- Event{Type: EventToolUse, ToolName: "Read", ToolInput: "a.go"}
	agentSession.events <- Event{Type: EventResult, Content: "done", Done: true}
	e.processInteractiveEvents(state, session, e.sessions, sessionKey, "m-tool-split", time.Now(), nil, nil, state.replyCtx, 0)

	var progressStarts []string
	for _, s := range p.getPreviewStarts() {
		if strings.Contains(s, "```#") {
			progressStarts = append(progressStarts, s)
		}
	}
	if len(progressStarts) != 2 {
		t.Fatalf("progress messages = %d, want 2 (one per segment); starts=%q", len(progressStarts), p.getPreviewStarts())
	}
	if !strings.HasPrefix(progressStarts[1], "```#2·Read") || strings.Contains(progressStarts[1], "#1·") {
		t.Fatalf("second progress message should hold only #2:\n%s", progressStarts[1])
	}
}
