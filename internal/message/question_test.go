package message

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeQuestion(t *testing.T) {
	tests := []struct {
		name string
		q    Block
	}{
		{"options", Block{Type: BlockQuestion, Text: "Deploy v2?", Options: []string{"Yes", "No"}}},
		{"typed reply", Block{Type: BlockQuestion, Text: "What should the new name be?"}},
		{"webhook", Block{Type: BlockQuestion, Text: "Ok?", Options: []string{"Ok"}, Webhook: "https://app.example/hook?t=1"}},
		{"max options", Block{Type: BlockQuestion, Text: "Pick", Options: strings.Split("abcdefghij", "")}},
		{"long option", Block{Type: BlockQuestion, Text: "Pick", Options: []string{strings.Repeat("o", MaxOptionLen)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := Normalize(Request{To: []string{"ali"}, Blocks: []Block{{Type: BlockText, Text: "ctx"}, tt.q}})
			if err != nil {
				t.Fatal(err)
			}
			q, ok := Question(got.Blocks)
			if !ok || q.Text != tt.q.Text || len(q.Options) != len(tt.q.Options) || q.Webhook != tt.q.Webhook {
				t.Errorf("Question = %+v, %v", q, ok)
			}
		})
	}
}

func TestNormalizeRejectsBadQuestions(t *testing.T) {
	q := func(text string, opts []string, hook string) Block {
		return Block{Type: BlockQuestion, Text: text, Options: opts, Webhook: hook}
	}
	tests := []struct {
		name   string
		blocks []Block
		want   string
	}{
		{"no text", []Block{q("", nil, "")}, "blocks[0].text: required"},
		{"blank text", []Block{q(" \n ", nil, "")}, "blocks[0].text: required"},
		{"long text", []Block{q(strings.Repeat("x", MaxTextLen+1), nil, "")}, "blocks[0].text: longer than"},
		{"empty options list", []Block{q("?", []string{}, "")}, "options: must have 1-10"},
		{"too many options", []Block{q("?", strings.Split("abcdefghijk", ""), "")}, "options: must have 1-10"},
		{"empty option", []Block{q("?", []string{"Yes", ""}, "")}, "options[1]: required"},
		{"blank option", []Block{q("?", []string{"  "}, "")}, "options[0]: required"},
		{"long option", []Block{q("?", []string{strings.Repeat("o", MaxOptionLen+1)}, "")}, "options[0]: longer than 64"},
		{"duplicate option", []Block{q("?", []string{"Yes", "No", "Yes"}, "")}, "options[2]: duplicate"},
		{"http webhook", []Block{q("?", nil, "http://app.example/hook")}, "webhook: must be an https URL"},
		{"relative webhook", []Block{q("?", nil, "/hook")}, "webhook: must be an https URL"},
		{"webhook no host", []Block{q("?", nil, "https://")}, "webhook: must be an https URL"},
		{"two questions", []Block{q("a?", nil, ""), q("b?", nil, "")}, "at most 1 question"},
		{"foreign field", []Block{{Type: BlockQuestion, Text: "?", URL: "https://a.b"}}, "don't belong"},
		{"options on text block", []Block{{Type: BlockText, Text: "x", Options: []string{"a"}}}, "don't belong"},
		{"webhook on link block", []Block{{Type: BlockLink, Text: "x", URL: "https://a.b", Webhook: "https://a.b"}}, "don't belong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Normalize(Request{To: []string{"ali"}, Blocks: tt.blocks})
			var ve *ValidationError
			if !errors.As(err, &ve) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestQuestionErrorsNeverEchoValues(t *testing.T) {
	_, _, err := Normalize(Request{To: []string{"ali"}, Blocks: []Block{{
		Type: BlockQuestion, Text: "SECRET?", Options: []string{"SECRET-a", "SECRET-a"}, Webhook: "http://SECRET.example",
	}}})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
}

func TestQuestionHelper(t *testing.T) {
	if _, ok := Question(nil); ok {
		t.Error("Question(nil) found one")
	}
	if _, ok := Question([]Block{{Type: BlockText, Text: "x"}}); ok {
		t.Error("Question found one in text-only blocks")
	}
	q, ok := Question([]Block{{Type: BlockText}, {Type: BlockQuestion, Text: "a"}})
	if !ok || q.Text != "a" {
		t.Errorf("Question = %+v, %v", q, ok)
	}
}
