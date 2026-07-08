package feishu

import (
	"testing"

	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestParseText(t *testing.T) {
	got, err := parseText("text", `{"text":"hello &amp; codex"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello & codex" {
		t.Fatalf("got %q", got)
	}
}

func TestParsePost(t *testing.T) {
	raw := `{"zh_cn":{"title":"标题","content":[[{"tag":"text","text":"看 "},{"tag":"a","text":"这里","href":"https://example.com"}],[{"tag":"code_block","text":"go test ./..."}]]}}`
	got := parsePost(raw)
	if len(got) != 3 {
		t.Fatalf("len=%d got=%#v", len(got), got)
	}
	if got[0] != "标题" || got[1] != "看 [这里](https://example.com)" {
		t.Fatalf("unexpected %#v", got)
	}
}

func TestStripMentionsOnlyBot(t *testing.T) {
	bot := "ou_bot"
	other := "ou_other"
	botKey := "@_bot"
	otherKey := "@_other"
	mentions := []*larkim.MentionEvent{
		{Key: &botKey, Id: &larkim.UserId{OpenId: &bot}},
		{Key: &otherKey, Id: &larkim.UserId{OpenId: &other}},
	}
	got := stripMentions("@_bot assign to @_other", mentions, bot)
	if got != "assign to @_other" {
		t.Fatalf("got %q", got)
	}
}

func TestMenuCommandMapping(t *testing.T) {
	tests := map[string]string{
		"session_new":          "/new",
		"session_list":         "/sessions",
		"session_current":      "/status",
		"exec_stop":            "/stop",
		"exec_workdir":         "/pwd",
		"settings_mode":        "/mode",
		"settings_model":       "/model",
		"settings_help":        "/help",
		"display_thinking_on":  "/display thinking",
		"display_thinking_off": "/display final",
		"display_minimal":      "/display quiet",
	}
	for eventKey, want := range tests {
		if got := menuCommand(eventKey); got != want {
			t.Fatalf("%s -> %q, want %q", eventKey, got, want)
		}
	}
}

func TestConvertBotMenu(t *testing.T) {
	p := &Platform{allowUsers: "*"}
	eventKey := "session_new"
	openID := "ou_user"
	msg, ok := p.convertBotMenu(&larkapplication.P2BotMenuV6{
		Event: &larkapplication.P2BotMenuV6Data{
			EventKey: &eventKey,
			Operator: &larkapplication.Operator{
				OperatorId: &larkapplication.UserId{OpenId: &openID},
			},
		},
	})
	if !ok {
		t.Fatal("menu was not converted")
	}
	if msg.Text != "/new" || msg.UserID != openID || msg.ChatType != "menu" {
		t.Fatalf("msg=%#v", msg)
	}
	rc, ok := msg.ReplyCtx.(ReplyContext)
	if !ok {
		t.Fatalf("reply ctx=%T", msg.ReplyCtx)
	}
	if rc.ReceiveID != openID || rc.ReceiveIDType != larkim.ReceiveIdTypeOpenId {
		t.Fatalf("reply ctx=%#v", rc)
	}
}
