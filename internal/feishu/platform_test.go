package feishu

import (
	"testing"

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
