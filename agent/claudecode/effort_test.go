package claudecode

import "testing"

func TestNormalizeEffort_XHigh(t *testing.T) {
	cases := map[string]string{
		"xhigh":      "xhigh",
		"XHigh":      "xhigh",
		" x-high ":   "xhigh",
		"extra-high": "xhigh",
		"extra_high": "xhigh",
		"max":        "max",
		"high":       "high",
		"bogus":      "",
	}
	for in, want := range cases {
		if got := normalizeEffort(in); got != want {
			t.Errorf("normalizeEffort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAvailableReasoningEfforts_IncludesXHigh(t *testing.T) {
	a := &Agent{}
	got := a.AvailableReasoningEfforts()
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if len(got) != len(want) {
		t.Fatalf("AvailableReasoningEfforts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AvailableReasoningEfforts() = %v, want %v", got, want)
		}
	}
}

func TestNew_ReadsXHighReasoningEffortFromConfig(t *testing.T) {
	ag, err := New(map[string]any{"cmd": "sh", "work_dir": t.TempDir(), "reasoning_effort": "xhigh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := ag.(*Agent).GetReasoningEffort(); got != "xhigh" {
		t.Fatalf("GetReasoningEffort() = %q, want xhigh", got)
	}
}
