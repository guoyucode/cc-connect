package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

// richTestServer records Bot API calls and answers them via respond, keyed by
// method name. Unlisted methods succeed with message_id 7.
type richTestServer struct {
	calls   []string
	bodies  []map[string]any
	respond map[string]string
}

func newRichTestPlatform(t *testing.T, respond map[string]string) (*Platform, *richTestServer) {
	t.Helper()
	srv := &richTestServer{respond: respond}
	p := newTelegramTestPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		method := path.Base(r.URL.Path)
		body := map[string]any{}
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			for k, v := range r.MultipartForm.Value {
				var decoded any
				if json.Unmarshal([]byte(v[0]), &decoded) == nil {
					if _, isObj := decoded.(map[string]any); isObj {
						body[k] = decoded
						continue
					}
					if n, isNum := decoded.(float64); isNum {
						body[k] = n
						continue
					}
				}
				body[k] = v[0]
			}
		}
		srv.calls = append(srv.calls, method)
		srv.bodies = append(srv.bodies, body)
		if resp, ok := srv.respond[method]; ok {
			fmt.Fprint(w, resp)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":7}}`)
	})
	p.richMessages = true
	return p, srv
}

const (
	badRequest = `{"ok":false,"error_code":400,"description":"Bad Request: can't parse rich message"}`
	notFound   = `{"ok":false,"error_code":404,"description":"Not Found"}`
	serverErr  = `{"ok":false,"error_code":500,"description":"Internal Server Error"}`
)

func TestReplySendsRichMarkdown(t *testing.T) {
	p, srv := newRichTestPlatform(t, nil)
	md := "| a | b |\n|---|---|\n| 1 | 2 |"
	if err := p.Reply(context.Background(), replyContext{chatID: 5, messageID: 3}, "\x1b[31m"+md); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if strings.Join(srv.calls, ",") != "sendRichMessage" {
		t.Fatalf("calls = %v, want [sendRichMessage]", srv.calls)
	}
	rich, _ := srv.bodies[0]["rich_message"].(map[string]any)
	if rich["markdown"] != md {
		t.Fatalf("markdown = %q, want ANSI-stripped table %q", rich["markdown"], md)
	}
	reply, _ := srv.bodies[0]["reply_parameters"].(map[string]any)
	if reply["message_id"] != float64(3) {
		t.Fatalf("reply_parameters = %v, want message_id 3", srv.bodies[0]["reply_parameters"])
	}
}

func TestReplyFallsBackToHTMLOnBadRequest(t *testing.T) {
	p, srv := newRichTestPlatform(t, map[string]string{"sendRichMessage": badRequest})
	if err := p.Reply(context.Background(), replyContext{chatID: 5, messageID: 3}, "**hi**"); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if strings.Join(srv.calls, ",") != "sendRichMessage,sendMessage" {
		t.Fatalf("calls = %v, want rich then HTML", srv.calls)
	}
	if srv.bodies[1]["text"] != "<b>hi</b>" || srv.bodies[1]["parse_mode"] != "HTML" {
		t.Fatalf("fallback body = %v, want plain MarkdownToSimpleHTML output", srv.bodies[1])
	}
	if !p.useRich() {
		t.Fatal("a per-message rejection must not disable rich messages")
	}
}

func TestReplyDoesNotFallBackOnServerError(t *testing.T) {
	p, srv := newRichTestPlatform(t, map[string]string{"sendRichMessage": serverErr})
	if err := p.Reply(context.Background(), replyContext{chatID: 5}, "hi"); err == nil {
		t.Fatal("Reply succeeded, want error")
	}
	if strings.Join(srv.calls, ",") != "sendRichMessage" {
		t.Fatalf("calls = %v; an ambiguous failure must not be re-sent as HTML", srv.calls)
	}
}

func TestRichDisabledAfterUnknownMethod(t *testing.T) {
	p, srv := newRichTestPlatform(t, map[string]string{"sendRichMessage": notFound})
	ctx := context.Background()
	rc := replyContext{chatID: 5}
	if err := p.Send(ctx, rc, "one"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := p.Send(ctx, rc, "two"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := strings.Join(srv.calls, ","); got != "sendRichMessage,sendMessage,sendMessage" {
		t.Fatalf("calls = %v, want rich tried once then HTML only", got)
	}
}

func TestRichMessagesOptOut(t *testing.T) {
	p, srv := newRichTestPlatform(t, nil)
	p.richMessages = false
	if err := p.Send(context.Background(), replyContext{chatID: 5}, "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if strings.Join(srv.calls, ",") != "sendMessage" {
		t.Fatalf("calls = %v, want HTML only", srv.calls)
	}
}

func TestRichPreviewStreamsViaRichEdits(t *testing.T) {
	p, srv := newRichTestPlatform(t, nil)
	ctx := context.Background()
	h, err := p.SendPreviewStart(ctx, replyContext{chatID: 5}, "| a |")
	if err != nil {
		t.Fatalf("SendPreviewStart: %v", err)
	}
	if !h.(*telegramPreviewHandle).rich {
		t.Fatal("preview handle not marked rich")
	}
	if err := p.UpdateMessage(ctx, h, "| a | b |\n|---|---|"); err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if got := strings.Join(srv.calls, ","); got != "sendRichMessage,editMessageText" {
		t.Fatalf("calls = %v", got)
	}
	edit := srv.bodies[1]
	rich, _ := edit["rich_message"].(map[string]any)
	if rich["markdown"] != "| a | b |\n|---|---|" || edit["message_id"] != float64(7) {
		t.Fatalf("edit body = %v, want rich_message edit of message 7", edit)
	}
	if _, hasText := edit["text"]; hasText && edit["text"] != "" {
		t.Fatalf("rich edit must not carry text: %v", edit)
	}
}

func TestRichPreviewEditNotModifiedIsSuccess(t *testing.T) {
	p, srv := newRichTestPlatform(t, map[string]string{
		"editMessageText": `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`,
	})
	h := &telegramPreviewHandle{chatID: 5, messageID: 7, rich: true}
	if err := p.UpdateMessage(context.Background(), h, "same"); err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if len(srv.calls) != 1 {
		t.Fatalf("calls = %v, want a single rich edit", srv.calls)
	}
}

func TestLegacyPreviewKeepsHTMLEdits(t *testing.T) {
	p, srv := newRichTestPlatform(t, nil)
	h := &telegramPreviewHandle{chatID: 5, messageID: 7}
	if err := p.UpdateMessage(context.Background(), h, "**x**"); err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if srv.calls[0] != "editMessageText" || srv.bodies[0]["parse_mode"] != "HTML" {
		t.Fatalf("edit = %v %v, want HTML edit for a non-rich preview", srv.calls, srv.bodies[0])
	}
}

func TestSendWithButtonsStaysOnHTML(t *testing.T) {
	p, srv := newRichTestPlatform(t, nil)
	if err := p.SendWithButtons(context.Background(), replyContext{chatID: 5}, "ok?", nil); err != nil {
		t.Fatalf("SendWithButtons: %v", err)
	}
	if strings.Join(srv.calls, ",") != "sendMessage" {
		t.Fatalf("calls = %v, want sendMessage", srv.calls)
	}
}

func TestNewRichMessagesOption(t *testing.T) {
	for _, tc := range []struct {
		opts map[string]any
		want bool
	}{
		{map[string]any{"token": "t"}, true},
		{map[string]any{"token": "t", "rich_messages": false}, false},
		{map[string]any{"token": "t", "rich_messages": true}, true},
	} {
		pl, err := New(tc.opts)
		if err != nil {
			t.Fatalf("New(%v): %v", tc.opts, err)
		}
		if got := pl.(*Platform).richMessages; got != tc.want {
			t.Errorf("New(%v).richMessages = %v, want %v", tc.opts, got, tc.want)
		}
	}
}

func TestToolBlockProgressFollowsRich(t *testing.T) {
	p, _ := newRichTestPlatform(t, nil)
	if !p.ToolBlockProgress() {
		t.Fatal("ToolBlockProgress = false with rich messages on")
	}
	p.richMessages = false
	if p.ToolBlockProgress() {
		t.Fatal("ToolBlockProgress = true on the HTML path")
	}
}

// A finished reply must replace its streaming preview in place: with the
// default delete-and-resend the message visibly flashes.
func TestFinishedPreviewIsEditedInPlace(t *testing.T) {
	var _ core.PreviewFinishPreference = (*Platform)(nil)
	var _ core.PreviewCleaner = (*Platform)(nil)
	if !(&Platform{}).KeepPreviewOnFinish() {
		t.Fatal("KeepPreviewOnFinish = false")
	}
}
