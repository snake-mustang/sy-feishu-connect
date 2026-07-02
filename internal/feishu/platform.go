package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"sy-feishu-codex-webhook/internal/bridge"
	"sy-feishu-codex-webhook/internal/util"
)

type Options struct {
	AppID          string
	AppSecret      string
	Domain         string
	RequireMention bool
	AllowUsers     string
	AllowChats     string
	WorkingEmoji   string
	DoneEmoji      string
	MaxReplyChars  int
}

type Platform struct {
	appID          string
	appSecret      string
	requireMention bool
	allowUsers     string
	allowChats     string
	workingEmoji   string
	doneEmoji      string
	maxReplyChars  int
	client         *lark.Client
	wsClient       *larkws.Client
	dispatcher     *dispatcher.EventDispatcher
	botOpenID      string
	domain         string
	dedup          *dedup
}

type ReplyContext struct {
	MessageID string
	ChatID    string
}

func New(opts Options) (*Platform, error) {
	if strings.TrimSpace(opts.AppID) == "" || strings.TrimSpace(opts.AppSecret) == "" {
		return nil, fmt.Errorf("feishu: app_id and app_secret are required")
	}
	domain := strings.ToLower(strings.TrimSpace(opts.Domain))
	if domain == "" {
		domain = "feishu"
	}
	var clientOpts []lark.ClientOptionFunc
	switch domain {
	case "feishu":
	case "lark":
		clientOpts = append(clientOpts, lark.WithOpenBaseUrl(lark.LarkBaseUrl))
	default:
		if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
			clientOpts = append(clientOpts, lark.WithOpenBaseUrl(domain))
		} else {
			return nil, fmt.Errorf("feishu: unsupported domain %q", opts.Domain)
		}
	}
	if opts.MaxReplyChars <= 0 {
		opts.MaxReplyChars = 3500
	}
	return &Platform{
		appID:          strings.TrimSpace(opts.AppID),
		appSecret:      strings.TrimSpace(opts.AppSecret),
		requireMention: opts.RequireMention,
		allowUsers:     opts.AllowUsers,
		allowChats:     opts.AllowChats,
		workingEmoji:   strings.TrimSpace(opts.WorkingEmoji),
		doneEmoji:      strings.TrimSpace(opts.DoneEmoji),
		maxReplyChars:  opts.MaxReplyChars,
		client:         lark.NewClient(opts.AppID, opts.AppSecret, clientOpts...),
		domain:         domain,
		dedup:          newDedup(10 * time.Minute),
	}, nil
}

func (p *Platform) Start(ctx context.Context, handler func(context.Context, bridge.Message)) error {
	openID, err := p.fetchBotOpenID(ctx)
	if err != nil {
		slog.Warn("feishu: failed to fetch bot open_id; group mention filtering may be disabled", "error", err)
	} else {
		p.botOpenID = openID
		slog.Info("feishu: bot identified", "open_id", openID)
	}

	p.dispatcher = dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			msg, ok := p.convertMessage(event)
			if !ok {
				return nil
			}
			go handler(ctx, msg)
			return nil
		})

	wsOpts := []larkws.ClientOption{
		larkws.WithEventHandler(p.dispatcher),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
		larkws.WithLogger(&sdkLogger{}),
	}
	if p.domain == "lark" {
		wsOpts = append(wsOpts, larkws.WithDomain(lark.LarkBaseUrl))
	} else if strings.HasPrefix(p.domain, "http://") || strings.HasPrefix(p.domain, "https://") {
		wsOpts = append(wsOpts, larkws.WithDomain(p.domain))
	}
	p.wsClient = larkws.NewClient(p.appID, p.appSecret, wsOpts...)

	go func() {
		if err := p.wsClient.Start(ctx); err != nil && ctx.Err() == nil {
			slog.Error("feishu: websocket stopped", "error", err)
		}
	}()
	return nil
}

func (p *Platform) convertMessage(event *larkim.P2MessageReceiveV1) (bridge.Message, bool) {
	if event == nil || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return bridge.Message{}, false
	}
	raw := event.Event.Message
	msgType := stringValue(raw.MessageType)
	if msgType != "text" && msgType != "post" {
		slog.Debug("feishu: ignoring unsupported message type", "type", msgType)
		return bridge.Message{}, false
	}

	messageID := stringValue(raw.MessageId)
	if messageID != "" && p.dedup.Seen(messageID) {
		slog.Debug("feishu: duplicate message ignored", "message_id", messageID)
		return bridge.Message{}, false
	}
	chatID := stringValue(raw.ChatId)
	userID := userIDFromEvent(event.Event.Sender.SenderId)
	chatType := stringValue(raw.ChatType)

	if !util.Allowed(p.allowUsers, userID) {
		slog.Info("feishu: unauthorized user ignored", "user_id", userID)
		return bridge.Message{}, false
	}
	if chatType == "group" && !util.Allowed(p.allowChats, chatID) {
		slog.Info("feishu: unauthorized chat ignored", "chat_id", chatID)
		return bridge.Message{}, false
	}
	if chatType == "group" && p.requireMention && p.botOpenID != "" && !botMentioned(raw.Mentions, p.botOpenID) {
		slog.Debug("feishu: group message without bot mention ignored", "chat_id", chatID)
		return bridge.Message{}, false
	}

	content := stringValue(raw.Content)
	text, err := parseText(msgType, content)
	if err != nil {
		slog.Warn("feishu: parse message failed", "message_id", messageID, "error", err)
		return bridge.Message{}, false
	}
	text = stripMentions(text, raw.Mentions, p.botOpenID)
	if strings.TrimSpace(text) == "" {
		return bridge.Message{}, false
	}

	sessionKey := "feishu:" + chatID + ":" + userID
	if chatType == "group" {
		sessionKey = "feishu:" + chatID + ":" + userID
	}
	return bridge.Message{
		SessionKey: sessionKey,
		MessageID:  messageID,
		ChatID:     chatID,
		ChatType:   chatType,
		UserID:     userID,
		Text:       strings.TrimSpace(text),
		ReplyCtx:   ReplyContext{MessageID: messageID, ChatID: chatID},
	}, true
}

func (p *Platform) Send(ctx context.Context, replyCtx any, content string) error {
	rc, ok := replyCtx.(ReplyContext)
	if !ok {
		return fmt.Errorf("feishu: invalid reply context %T", replyCtx)
	}
	chunks := util.Chunks(content, p.maxReplyChars)
	if len(chunks) == 0 {
		chunks = []string{"(empty response)"}
	}
	for _, chunk := range chunks {
		if err := p.sendOne(ctx, rc, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (p *Platform) sendOne(ctx context.Context, rc ReplyContext, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	if rc.MessageID != "" {
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(rc.MessageID).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType(larkim.MsgTypeText).
				Content(string(body)).
				Build()).
			Build()
		resp, err := p.client.Im.Message.Reply(ctx, req)
		if err != nil {
			return fmt.Errorf("feishu: reply api: %w", err)
		}
		if !resp.Success() {
			return fmt.Errorf("feishu: reply failed code=%d msg=%s", resp.Code, resp.Msg)
		}
		return nil
	}
	if rc.ChatID == "" {
		return fmt.Errorf("feishu: no message_id or chat_id for reply")
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(rc.ChatID).
			MsgType(larkim.MsgTypeText).
			Content(string(body)).
			Build()).
		Build()
	resp, err := p.client.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: create message api: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: create message failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (p *Platform) ReactWorking(ctx context.Context, replyCtx any) error {
	return p.react(ctx, replyCtx, p.workingEmoji)
}

func (p *Platform) ReactDone(ctx context.Context, replyCtx any) error {
	return p.react(ctx, replyCtx, p.doneEmoji)
}

func (p *Platform) react(ctx context.Context, replyCtx any, emoji string) error {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return nil
	}
	rc, ok := replyCtx.(ReplyContext)
	if !ok || rc.MessageID == "" {
		return nil
	}
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(rc.MessageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emoji).Build()).
			Build()).
		Build()
	resp, err := p.client.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		slog.Debug("feishu: reaction api failed", "emoji", emoji, "error", err)
		return nil
	}
	if !resp.Success() {
		slog.Debug("feishu: reaction rejected", "emoji", emoji, "code", resp.Code, "msg", resp.Msg)
	}
	return nil
}

func (p *Platform) fetchBotOpenID(ctx context.Context) (string, error) {
	resp, err := p.client.Get(ctx, "/open-apis/bot/v3/info", nil, larkcore.AccessTokenTypeTenant)
	if err != nil {
		return "", err
	}
	var result struct {
		Code int `json:"code"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(resp.RawBody, &result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("code=%d msg=%s", result.Code, result.Msg)
	}
	return result.Bot.OpenID, nil
}

func parseText(msgType, raw string) (string, error) {
	switch msgType {
	case "text":
		var body struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			return "", err
		}
		return html.UnescapeString(body.Text), nil
	case "post":
		parts := parsePost(raw)
		return strings.Join(parts, "\n"), nil
	default:
		return "", nil
	}
}

func parsePost(raw string) []string {
	type elem struct {
		Tag      string `json:"tag"`
		Text     string `json:"text"`
		Href     string `json:"href"`
		UserName string `json:"user_name"`
		UserID   string `json:"user_id"`
	}
	type post struct {
		Title   string   `json:"title"`
		Content [][]elem `json:"content"`
		ZhCN    *post    `json:"zh_cn"`
		EnUS    *post    `json:"en_us"`
	}
	var p post
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil
	}
	if p.ZhCN != nil {
		p = *p.ZhCN
	} else if p.EnUS != nil {
		p = *p.EnUS
	}
	var parts []string
	if strings.TrimSpace(p.Title) != "" {
		parts = append(parts, p.Title)
	}
	for _, line := range p.Content {
		var b strings.Builder
		for _, e := range line {
			switch e.Tag {
			case "text", "markdown", "md":
				b.WriteString(e.Text)
			case "a":
				if e.Href != "" {
					b.WriteString(fmt.Sprintf("[%s](%s)", e.Text, e.Href))
				} else {
					b.WriteString(e.Text)
				}
			case "at":
				if e.UserName != "" {
					b.WriteString("@" + e.UserName)
				} else if e.UserID != "" {
					b.WriteString("@" + e.UserID)
				}
			case "code_block":
				b.WriteString("\n```\n" + e.Text + "\n```\n")
			}
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			parts = append(parts, html.UnescapeString(s))
		}
	}
	return parts
}

func stripMentions(text string, mentions []*larkim.MentionEvent, botOpenID string) string {
	out := text
	for _, m := range mentions {
		if m == nil {
			continue
		}
		if botOpenID != "" && m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId != botOpenID {
			continue
		}
		if m.Key != nil && *m.Key != "" {
			out = strings.ReplaceAll(out, *m.Key, "")
		}
	}
	out = strings.ReplaceAll(out, "@_all", "")
	return strings.TrimSpace(out)
}

func botMentioned(mentions []*larkim.MentionEvent, botOpenID string) bool {
	for _, m := range mentions {
		if m != nil && m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId == botOpenID {
			return true
		}
	}
	return false
}

func userIDFromEvent(id *larkim.UserId) string {
	if id == nil {
		return ""
	}
	if id.OpenId != nil && *id.OpenId != "" {
		return *id.OpenId
	}
	if id.UserId != nil && *id.UserId != "" {
		return *id.UserId
	}
	if id.UnionId != nil && *id.UnionId != "" {
		return *id.UnionId
	}
	return ""
}

func stringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

type dedup struct {
	mu   sync.Mutex
	ttl  time.Duration
	seen map[string]time.Time
}

func newDedup(ttl time.Duration) *dedup {
	return &dedup{ttl: ttl, seen: map[string]time.Time{}}
}

func (d *dedup) Seen(id string) bool {
	if id == "" {
		return false
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, t := range d.seen {
		if now.Sub(t) > d.ttl {
			delete(d.seen, k)
		}
	}
	if _, ok := d.seen[id]; ok {
		return true
	}
	d.seen[id] = now
	return false
}

type sdkLogger struct{}

func (l *sdkLogger) Debug(ctx context.Context, args ...interface{}) {
	if containsNoise(args) {
		return
	}
	slog.Debug("feishu sdk", "args", args)
}
func (l *sdkLogger) Info(ctx context.Context, args ...interface{}) {
	slog.Info("feishu sdk", "args", args)
}
func (l *sdkLogger) Warn(ctx context.Context, args ...interface{}) {
	slog.Warn("feishu sdk", "args", args)
}
func (l *sdkLogger) Error(ctx context.Context, args ...interface{}) {
	slog.Error("feishu sdk", "args", args)
}

func containsNoise(args []interface{}) bool {
	for _, arg := range args {
		s, ok := arg.(string)
		if !ok {
			continue
		}
		s = strings.ToLower(s)
		if strings.Contains(s, "ping success") || strings.Contains(s, "receive pong") {
			return true
		}
	}
	return false
}
