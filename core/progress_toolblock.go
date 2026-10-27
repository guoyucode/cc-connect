package core

import (
	"strconv"
	"strings"
)

// ToolBlockProgressSupport is an optional interface for platforms whose
// compact progress message should show each tool call as a single code block
// holding the tool input, its status and its output. The block is rendered
// when the tool starts and rewritten in place once its result arrives.
type ToolBlockProgressSupport interface {
	ToolBlockProgress() bool
}

func toolBlockProgressEnabled(p Platform) bool {
	s, ok := p.(ToolBlockProgressSupport)
	return ok && s.ToolBlockProgress()
}

// toolBlockProgress keeps compact progress entries as separate blocks so a
// tool's block can be replaced when its result arrives.
type toolBlockProgress struct {
	i18n    *I18n
	blocks  []string
	pending []pendingToolBlock // tool calls awaiting results, oldest first
	seq     int
}

type pendingToolBlock struct {
	block int
	seq   int
	use   ProgressCardEntry
}

func newToolBlockProgress(lang Language) *toolBlockProgress {
	return &toolBlockProgress{i18n: NewI18n(lang)}
}

// append records one progress entry and returns the full progress markdown.
// Tool results carry no tool-call ID, so they are matched to pending tool
// calls in order, which is the order agents report them in.
func (t *toolBlockProgress) append(item ProgressCardEntry, fallback string) string {
	// AppendStructured substitutes fallback for empty entry text.
	text := item.Text
	if text == fallback {
		text = ""
	}
	switch {
	case item.Kind == ProgressEntryToolUse:
		t.seq++
		item.Text = text
		t.pending = append(t.pending, pendingToolBlock{block: len(t.blocks), seq: t.seq, use: item})
		t.blocks = append(t.blocks, t.render(t.seq, item, nil))
	case item.Kind == ProgressEntryToolResult && len(t.pending) > 0:
		pt := t.pending[0]
		t.pending = t.pending[1:]
		item.Text = text
		t.blocks[pt.block] = t.render(pt.seq, pt.use, &item)
	default:
		t.blocks = append(t.blocks, fallback)
	}
	return strings.Join(t.blocks, "\n\n")
}

func (t *toolBlockProgress) render(seq int, use ProgressCardEntry, result *ProgressCardEntry) string {
	var lines []string
	if use.Text != "" {
		prefix := "> "
		switch use.Tool {
		case "Bash", "shell", "run_shell_command":
			prefix = "$ "
		}
		lines = append(lines, prefix+use.Text)
	}
	status := "⏳ …"
	if result != nil {
		status = t.status(result)
	}
	lines = append(lines, "──────── "+status+" ────────")
	if result != nil && result.Text != "" {
		lines = append(lines, result.Text)
	}
	body := strings.Join(lines, "\n")
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return fence + "#" + strconv.Itoa(seq) + "·" + use.Tool + "\n" + body + "\n" + fence
}

func (t *toolBlockProgress) status(r *ProgressCardEntry) string {
	dot, s := "🧾", strings.TrimSpace(r.Status)
	if r.Success != nil {
		if *r.Success {
			dot = "🟢"
			if s == "" {
				s = t.i18n.T(MsgToolResultFmtOk)
			}
		} else {
			dot = "🔴"
			if s == "" {
				s = t.i18n.T(MsgToolResultFmtFailed)
			}
		}
	}
	parts := []string{dot}
	if s != "" {
		parts[0] += " " + s
	}
	if r.ExitCode != nil {
		parts = append(parts, t.i18n.T(MsgToolResultFmtExit)+" "+strconv.Itoa(*r.ExitCode))
	}
	return strings.Join(parts, " · ")
}
