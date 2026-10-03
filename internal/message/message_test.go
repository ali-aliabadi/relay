package message

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var (
	pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	pngB64   = base64.StdEncoding.EncodeToString(pngBytes)
	jpegB64  = base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3})
)

func valid() Request {
	return Request{To: []string{"ali"}, Blocks: []Block{{Type: BlockText, Text: "hi"}}}
}

func TestNormalizeDefaultsAndShorthand(t *testing.T) {
	got, images, err := Normalize(Request{To: []string{"ali"}, Text: "Deploy done"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Urgency != UrgencyNormal || len(got.Blocks) != 1 || got.Blocks[0].Type != BlockText ||
		got.Blocks[0].Text != "Deploy done" || got.Text != "" || len(images) != 0 {
		t.Errorf("normalized = %+v", got)
	}
}

func TestNormalizeAllBlockTypes(t *testing.T) {
	req := Request{
		To: []string{"ali", "sara"}, Urgency: UrgencyHigh, Source: "backup-script", Title: "Backup failed",
		Channels: []string{"telegram"}, IdempotencyKey: "backup-2026-10-03",
		Blocks: []Block{
			{Type: BlockText, Text: "Nightly backup stopped."},
			{Type: BlockFields, Items: []Field{{Label: "Host", Value: "nas"}, {Label: "Error", Value: ""}}},
			{Type: BlockTable, Columns: []string{"Disk", "Used"}, Rows: [][]string{{"sda", "98%"}}},
			{Type: BlockImage, Base64: pngB64, ContentType: "image/png", Caption: "Disk usage"},
			{Type: BlockCode, Text: "rsync: write failed"},
			{Type: BlockLink, Text: "Open dashboard", URL: "https://grafana.example/d/disks"},
		},
	}
	got, images, err := Normalize(req)
	if err != nil {
		t.Fatal(err)
	}
	img := got.Blocks[3]
	if len(images) != 1 || string(images[0].Bytes) != string(pngBytes) || images[0].ContentType != "image/png" {
		t.Fatalf("images = %+v", images)
	}
	if img.Base64 != "" || img.Attachment == nil || *img.Attachment != 0 {
		t.Errorf("image block not rewritten: %+v", img)
	}
	if req.Blocks[3].Base64 == "" {
		t.Error("Normalize mutated the caller's blocks")
	}
	if _, _, err := Normalize(Request{To: []string{"ali"}, Blocks: []Block{
		{Type: BlockImage, URL: "https://example.com/a.png"},
		{Type: BlockImage, Base64: jpegB64, ContentType: "image/jpeg"},
	}}); err == nil || !strings.Contains(err.Error(), "at most 1 image") {
		t.Errorf("two images: %v", err)
	}
}

func TestNormalizeRejects(t *testing.T) {
	long := func(n int) string { return strings.Repeat("x", n) }
	rows := make([][]string, MaxTableRows+1)
	for i := range rows {
		rows[i] = []string{"a"}
	}
	bigImage := base64.StdEncoding.EncodeToString(append(pngBytes, make([]byte, MaxImageBytes)...))
	blocks := make([]Block, MaxBlocks+1)
	for i := range blocks {
		blocks[i] = Block{Type: BlockText, Text: "x"}
	}
	one := func(b Block) func(*Request) { return func(r *Request) { r.Blocks = []Block{b} } }
	idx := 0
	tests := []struct {
		name string
		edit func(*Request)
		want string
	}{
		{"no recipients", func(r *Request) { r.To = nil }, "to: must list"},
		{"too many recipients", func(r *Request) { r.To = strings.Split(long(11), "") }, "to: must list"},
		{"duplicate recipient", func(r *Request) { r.To = []string{"ali", "ali"} }, "to[1]: duplicate"},
		{"empty recipient", func(r *Request) { r.To = []string{""} }, "to[0]: required"},
		{"bad urgency", func(r *Request) { r.Urgency = "urgent" }, "urgency"},
		{"duplicate channel", func(r *Request) { r.Channels = []string{"telegram", "telegram"} }, "channels[1]: duplicate"},
		{"long title", func(r *Request) { r.Title = long(MaxTitleLen + 1) }, "title: longer than"},
		{"long source", func(r *Request) { r.Source = long(MaxSourceLen + 1) }, "source: longer than"},
		{"long key", func(r *Request) { r.IdempotencyKey = long(MaxIdempotencyLen + 1) }, "idempotency_key"},
		{"no blocks", func(r *Request) { r.Blocks = nil }, "blocks: send text"},
		{"too many blocks", func(r *Request) { r.Blocks = blocks }, "at most 20 blocks"},
		{"text and blocks", func(r *Request) { r.Text = "x" }, "either text or blocks"},
		{"unknown type", one(Block{Type: "html", Text: "<b>"}), "blocks[0].type"},
		{"empty text", one(Block{Type: BlockText, Text: "  "}), "blocks[0].text: required"},
		{"long text", one(Block{Type: BlockText, Text: long(MaxTextLen + 1)}), "longer than 4000"},
		{"text with url", one(Block{Type: BlockText, Text: "x", URL: "https://a.b"}), "don't belong"},
		{"caller attachment", one(Block{Type: BlockText, Text: "x", Attachment: &idx}), "set by Relay"},
		{"no field items", one(Block{Type: BlockFields}), "items: must have"},
		{"empty label", one(Block{Type: BlockFields, Items: []Field{{Value: "v"}}}), "label: required"},
		{"no columns", one(Block{Type: BlockTable, Rows: [][]string{{"a"}}}), "columns: must have"},
		{"too many rows", one(Block{Type: BlockTable, Columns: []string{"c"}, Rows: rows}), "at most 50 rows"},
		{"ragged row", one(Block{Type: BlockTable, Columns: []string{"a", "b"}, Rows: [][]string{{"1"}}}), "must have 2 cells"},
		{"http link", one(Block{Type: BlockLink, Text: "x", URL: "http://a.b"}), "https URL"},
		{"js link", one(Block{Type: BlockLink, Text: "x", URL: "javascript:alert(1)"}), "https URL"},
		{"no link url", one(Block{Type: BlockLink, Text: "x"}), "url: required"},
		{"image nothing", one(Block{Type: BlockImage}), "needs url or base64"},
		{"image both", one(Block{Type: BlockImage, URL: "https://a.b/i.png", Base64: pngB64}), "not both"},
		{"image url type", one(Block{Type: BlockImage, URL: "https://a.b/i.png", ContentType: "image/png"}), "only used with base64"},
		{"image gif", one(Block{Type: BlockImage, Base64: pngB64, ContentType: "image/gif"}), "image/png or image/jpeg"},
		{"image bad b64", one(Block{Type: BlockImage, Base64: "!!!", ContentType: "image/png"}), "not valid standard base64"},
		{"image lies", one(Block{Type: BlockImage, Base64: jpegB64, ContentType: "image/png"}), "not a image/png"},
		{"image too big", one(Block{Type: BlockImage, Base64: bigImage, ContentType: "image/png"}), "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid()
			tt.edit(&req)
			_, _, err := Normalize(req)
			var ve *ValidationError
			if !errors.As(err, &ve) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestValidationErrorNeverEchoesValues(t *testing.T) {
	secret := "SECRET-" + strings.Repeat("y", MaxTitleLen)
	_, _, err := Normalize(Request{To: []string{"ali", "ali"}, Title: secret, Blocks: []Block{
		{Type: BlockLink, Text: "SECRET-link", URL: "http://SECRET.example"},
	}})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidationErrorIsCapped(t *testing.T) {
	blocks := make([]Block, 15)
	for i := range blocks {
		blocks[i] = Block{Type: "nope"}
	}
	_, _, err := Normalize(Request{To: []string{"ali"}, Blocks: blocks})
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != maxProblems {
		t.Errorf("problems = %d, want %d", len(ve.Problems), maxProblems)
	}
}

func TestLogValuesHideContent(t *testing.T) {
	r := Request{To: []string{"ali"}, Title: "SECRET", Blocks: []Block{{Type: BlockText, Text: "SECRET"}}}
	m := Message{Title: "SECRET", Blocks: r.Blocks}
	for _, v := range []any{r.LogValue().Any(), m.LogValue().Any()} {
		out, _ := json.Marshal(v)
		if strings.Contains(string(out), "SECRET") {
			t.Errorf("log value leaks content: %s", out)
		}
	}
}

func FuzzNormalize(f *testing.F) {
	f.Add(`{"to":["ali"],"text":"hi"}`)
	f.Add(`{"to":["ali"],"blocks":[{"type":"table","columns":["a"],"rows":[["1"]]}]}`)
	f.Add(`{"to":["ali"],"blocks":[{"type":"image","base64":"` + pngB64 + `","content_type":"image/png"}]}`)
	f.Add(`{"to":["ali"],"blocks":[{"type":"question","text":"ok?","options":["y","n"],"webhook":"https://a.b/h"}]}`)
	f.Fuzz(func(t *testing.T, body string) {
		var req Request
		if json.Unmarshal([]byte(body), &req) != nil {
			return
		}
		got, images, err := Normalize(req)
		if err != nil {
			return
		}
		for _, b := range got.Blocks {
			if b.Base64 != "" {
				t.Fatal("normalized block still carries base64")
			}
			if b.Attachment != nil && *b.Attachment >= len(images) {
				t.Fatal("attachment index out of range")
			}
			if b.Type == BlockQuestion && len(b.Options) > MaxOptions {
				t.Fatal("too many options passed validation")
			}
		}
	})
}
