// Package telegram is the Telegram channel: a Bot API client and the one
// layout that turns content blocks into Telegram's HTML subset.
package telegram

import (
	"html"
	"strings"
	"unicode/utf16"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

// Telegram's limits, counted in visible characters after entity parsing.
const (
	maxTextLen    = 4096
	maxCaptionLen = 1024
	ellipsis      = "…"
)

// frag is a complete HTML fragment and its visible length.
type frag struct {
	html    string
	visible int
}

func (f frag) empty() bool { return f.html == "" }

// visibleLen counts like Telegram: UTF-16 code units of the visible text.
func visibleLen(s string) int { return len(utf16.Encode([]rune(s))) }

// text escapes raw caller text. Every caller value goes through here.
func text(raw string) frag { return frag{html: html.EscapeString(raw), visible: visibleLen(raw)} }

// wrap puts f inside a tag, which adds no visible characters.
func wrap(tag string, f frag) frag {
	return frag{html: "<" + tag + ">" + f.html + "</" + tag + ">", visible: f.visible}
}

func join(sep string, fs ...frag) frag {
	var b strings.Builder
	out := frag{}
	for _, f := range fs {
		if f.empty() {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(sep)
			out.visible += visibleLen(sep)
		}
		b.WriteString(f.html)
		out.visible += f.visible
	}
	out.html = b.String()
	return out
}

// truncate shortens raw so its visible length is at most limit, ending in "…".
func truncate(raw string, limit int) string {
	if visibleLen(raw) <= limit {
		return raw
	}
	if limit <= 0 {
		return ""
	}
	runes := []rune(raw)
	n := 0
	budget := limit - visibleLen(ellipsis)
	for i, r := range runes {
		n += len(utf16.Encode([]rune{r}))
		if n > budget {
			return string(runes[:i]) + ellipsis
		}
	}
	return raw
}

// Render turns a message into the Telegram calls that send it.
func Render(msg message.Message) []channel.Part {
	header := renderHeader(msg)
	footer := renderFooter(msg)
	silent := msg.Urgency == message.UrgencyLow
	var buttons []channel.Button
	var media []channel.Part
	var blocks, rest []func(int) frag // rest leaves out media captions
	for i := range msg.Blocks {
		b := msg.Blocks[i]
		switch b.Type {
		case message.BlockLink:
			buttons = append(buttons, channel.Button{Text: truncate(b.Text, 64), URL: b.URL})
		case message.BlockQuestion:
			// Rendered in the footer so a long message never cuts it off.
			for j, o := range b.Options {
				buttons = append(buttons, channel.Button{Text: o, Data: AnswerData(msg.DeliveryID, j)})
			}
		case message.BlockImage, message.BlockFile:
			media = append(media, mediaPart(b, silent))
			if b.Caption != "" {
				blocks = append(blocks, inline("i", b.Caption))
			}
		default:
			blocks = append(blocks, blockRenderer(b))
			rest = append(rest, blockRenderer(b))
		}
	}

	if len(media) == 0 {
		body := fit(header, blocks, footer, maxTextLen)
		return []channel.Part{{Kind: "text", Text: body.html, Buttons: buttons, DisableNotification: silent}}
	}
	if full := fit(header, blocks, footer, maxTextLen); len(media) == 1 && full.visible <= maxCaptionLen {
		media[0].Text, media[0].Buttons = full.html, buttons
		return media
	}
	// Too long for one caption, or several media: each photo or file goes
	// first with its own caption, then the text (without those captions).
	body := fit(header, rest, footer, maxTextLen)
	if body.empty() {
		media[len(media)-1].Buttons = buttons
		return media
	}
	return append(media, channel.Part{Kind: "text", Text: body.html, Buttons: buttons, DisableNotification: silent})
}

// mediaPart is a photo or document part captioned with the block's own caption.
func mediaPart(b message.Block, silent bool) channel.Part {
	var caption string
	if b.Caption != "" {
		caption = text(truncate(b.Caption, maxCaptionLen)).html
	}
	p := channel.Part{Kind: "photo", Text: caption, DisableNotification: silent}
	switch {
	case b.Type == message.BlockFile:
		p.Kind, p.Document, p.Filename = "document", "inline", b.Filename
	case b.URL != "":
		p.Photo = b.URL
	default:
		p.Photo = "inline"
	}
	return p
}

func renderHeader(msg message.Message) frag {
	title := msg.Title
	if msg.Urgency == message.UrgencyCritical {
		title = strings.TrimSpace("🚨 " + title)
	}
	if title == "" {
		return frag{}
	}
	return wrap("b", text(truncate(title, 256)))
}

// maxQuestionLen keeps the question (always shown, in the footer) short
// enough to leave room for the rest of the message.
const maxQuestionLen = 1000

func renderFooter(msg message.Message) frag {
	var q frag
	if b, ok := message.Question(msg.Blocks); ok {
		q = join(" ", text("❓"), wrap("b", text(truncate(b.Text, maxQuestionLen))))
		if len(b.Options) == 0 {
			q = join("\n", q, wrap("i", text("Reply to this message to answer.")))
		}
	}
	var src frag
	if msg.Source != "" {
		src = wrap("i", text("via "+truncate(msg.Source, 64)))
	}
	return join("\n\n", q, src)
}

// fit joins header, blocks and footer with blank lines within limit visible
// characters. The first block that doesn't fit is shortened and the rest are
// dropped; header and footer always fit (they are short by validation).
func fit(header frag, blocks []func(int) frag, footer frag, limit int) frag {
	const sep = "\n\n"
	sepLen := visibleLen(sep)
	budget := limit - footer.visible - sepLen
	out := header
	for _, render := range blocks {
		room := budget - out.visible
		if !out.empty() {
			room -= sepLen
		}
		f := render(room)
		if f.empty() {
			break
		}
		out = join(sep, out, f)
		if out.visible >= budget-sepLen {
			break
		}
	}
	return join(sep, out, footer)
}
