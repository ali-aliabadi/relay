package telegram

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/message"
)

var update = flag.Bool("update", false, "rewrite golden files")

func idx(i int) *int { return &i }

var goldenCases = map[string]message.Message{
	"text": {Urgency: "normal", Blocks: []message.Block{{Type: "text", Text: "Deploy done: v1.2 is live"}}},
	"title_source": {
		Urgency: "normal", Title: "Backup", Source: "backup-script",
		Blocks: []message.Block{{Type: "text", Text: "All good."}},
	},
	"critical":   {Urgency: "critical", Title: "Disk full", Blocks: []message.Block{{Type: "text", Text: "nas is at 100%"}}},
	"low_silent": {Urgency: "low", Blocks: []message.Block{{Type: "text", Text: "fyi"}}},
	"escaping": {
		Urgency: "normal", Title: "<b>not bold</b> & co",
		Blocks: []message.Block{
			{Type: "text", Text: `<script>alert("x")</script> 1 < 2 && 3 > 2`},
			{Type: "fields", Items: []message.Field{{Label: "<i>L</i>", Value: "a&b"}}},
			{Type: "code", Text: "</pre><b>x</b>"},
		},
	},
	"fields": {Urgency: "normal", Blocks: []message.Block{{Type: "fields", Items: []message.Field{
		{Label: "Host", Value: "nas"}, {Label: "Error", Value: "disk full"}, {Label: "Empty", Value: ""},
	}}}},
	"table": {Urgency: "normal", Blocks: []message.Block{{
		Type: "table", Columns: []string{"Disk", "Used", "Mount point"},
		Rows: [][]string{{"sda", "98%", "/"}, {"sdb", "41%", "/srv/a-very-long-mount-point-name"}, {"nvme0n1", "7%", "/home\n/x"}},
	}}},
	"code": {Urgency: "normal", Blocks: []message.Block{{Type: "code", Text: "rsync: write failed\n  No space left"}}},
	"links": {Urgency: "normal", Blocks: []message.Block{
		{Type: "text", Text: "See:"},
		{Type: "link", Text: "Open dashboard", URL: "https://grafana.example/d/disks"},
		{Type: "link", Text: "Runbook", URL: "https://wiki.example/runbook?a=1&b=2"},
	}},
	"photo_url_caption": {Urgency: "high", Title: "Disk usage", Blocks: []message.Block{
		{Type: "image", URL: "https://grafana.example/render/disk.png", Caption: "Last 24h"},
		{Type: "text", Text: "sda is filling up."},
		{Type: "link", Text: "Open", URL: "https://grafana.example/d/disks"},
	}},
	"photo_inline": {Urgency: "normal", Blocks: []message.Block{{Type: "image", Attachment: idx(0)}}},
	"photo_long_text": {Urgency: "normal", Title: "Report", Blocks: []message.Block{
		{Type: "image", URL: "https://example.com/chart.png", Caption: "Chart"},
		{Type: "text", Text: strings.Repeat("long text ", 150)},
	}},
	"file_caption": {Urgency: "low", Title: "Report", Source: "billing", Blocks: []message.Block{
		{Type: "text", Text: "September invoice."},
		{Type: "file", Filename: "invoice <9>.pdf", ContentType: "application/pdf", Caption: "Due Oct 15", Attachment: idx(0)},
		{Type: "link", Text: "Pay", URL: "https://bank.example/pay"},
	}},
	"file_long_text": {Urgency: "normal", Title: "Logs", Blocks: []message.Block{
		{Type: "file", Filename: "build.log", ContentType: "text/plain", Caption: "Full log", Attachment: idx(0)},
		{Type: "code", Text: strings.Repeat("error line\n", 120)},
	}},
	"photo_and_file": {Urgency: "normal", DeliveryID: "dlv_TEST", Blocks: []message.Block{
		{Type: "image", URL: "https://example.com/chart.png", Caption: "Chart"},
		{Type: "file", Filename: "data.csv", ContentType: "text/csv", Attachment: idx(0)},
		{Type: "question", Text: "Approve?", Options: []string{"Yes"}},
	}},
	"photo_and_file_only": {Urgency: "normal", Blocks: []message.Block{
		{Type: "image", URL: "https://example.com/chart.png"},
		{Type: "file", Filename: "data.csv", Attachment: idx(0)},
		{Type: "link", Text: "Open", URL: "https://example.com"},
	}},
	"emoji": {Urgency: "normal", Blocks: []message.Block{{Type: "text", Text: "سلام 👋🏽 مرحبا"}}},
	"question_options": {Urgency: "high", Title: "Deploy", Source: "ci", DeliveryID: "dlv_TEST", Blocks: []message.Block{
		{Type: "text", Text: "v2 passed staging."},
		{Type: "question", Text: "Ship <v2> to prod?", Options: []string{"Yes", "No & wait"}, Webhook: "https://app.example/hook"},
		{Type: "link", Text: "Diff", URL: "https://git.example/diff"},
	}},
	"question_typed": {Urgency: "normal", DeliveryID: "dlv_TEST", Blocks: []message.Block{
		{Type: "question", Text: "What should the new hostname be?"},
	}},
	"question_long_text": {Urgency: "normal", DeliveryID: "dlv_TEST", Blocks: []message.Block{
		{Type: "text", Text: strings.Repeat("long text ", 500)},
		{Type: "question", Text: "Still there?", Options: []string{"Yes"}},
	}},
	"question_photo_long": {Urgency: "normal", DeliveryID: "dlv_TEST", Blocks: []message.Block{
		{Type: "image", URL: "https://example.com/chart.png", Caption: "Chart"},
		{Type: "text", Text: strings.Repeat("long text ", 150)},
		{Type: "question", Text: "Scale up?", Options: []string{"Yes", "No"}},
	}},
}

func TestGolden(t *testing.T) {
	for name, msg := range goldenCases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false) // keep the markup readable in review
			enc.SetIndent("", "  ")
			if err := enc.Encode(Render(msg)); err != nil {
				t.Fatal(err)
			}
			got := buf.Bytes()
			path := filepath.Join("testdata", name+".golden")
			if *update {
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden file (run go test -update): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("layout changed; run go test ./internal/channel/telegram -update and review the diff\ngot:\n%s", got)
			}
		})
	}
}

func TestLengthLimits(t *testing.T) {
	var blocks []message.Block
	for range 20 {
		blocks = append(blocks, message.Block{Type: "text", Text: strings.Repeat("&<>", 1300)})
	}
	rows := make([][]string, 50)
	for i := range rows {
		rows[i] = []string{strings.Repeat("x", 30), strings.Repeat("y", 30)}
	}
	blocks = append(blocks, message.Block{Type: "table", Columns: []string{"a", "b"}, Rows: rows})
	msg := message.Message{Urgency: "critical", Title: strings.Repeat("t", 256), Source: "src", Blocks: blocks}
	for _, p := range Render(msg) {
		if n := visibleLen(unescapeVisible(p.Text)); n > maxTextLen {
			t.Errorf("text part has %d visible characters, limit %d", n, maxTextLen)
		}
		if !strings.Contains(p.Text, "<i>via src</i>") {
			t.Error("footer dropped by truncation")
		}
		if !strings.HasSuffix(strings.TrimSuffix(p.Text, "\n\n<i>via src</i>"), "…") {
			t.Error("truncated text does not end with …")
		}
	}

	photo := message.Message{Urgency: "normal", Blocks: []message.Block{
		{Type: "image", URL: "https://e.x/p.png", Caption: strings.Repeat("c", 1024)},
		{Type: "text", Text: strings.Repeat("z", 2000)},
	}}
	parts := Render(photo)
	if len(parts) != 2 || visibleLen(unescapeVisible(parts[0].Text)) > maxCaptionLen {
		t.Errorf("long photo message parts = %d", len(parts))
	}
}

// unescapeVisible strips tags and decodes entities to measure what Telegram shows.
func unescapeVisible(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	r := strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&", "&#34;", `"`, "&#39;", "'")
	return r.Replace(b.String())
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in    string
		limit int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "hel…"},
		{"hello", 0, ""},
		{"👋👋👋", 4, "👋…"}, // each emoji is 2 UTF-16 units
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.limit); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
		}
	}
}

func FuzzRender(f *testing.F) {
	f.Add("title", "text <b>", "label", "value", "cell")
	f.Fuzz(func(t *testing.T, title, txt, label, value, cell string) {
		msg := message.Message{Urgency: "normal", Title: title, Blocks: []message.Block{
			{Type: "text", Text: txt},
			{Type: "fields", Items: []message.Field{{Label: label, Value: value}}},
			{Type: "table", Columns: []string{cell}, Rows: [][]string{{cell}}},
			{Type: "code", Text: txt},
		}}
		for _, p := range Render(msg) {
			if visibleLen(unescapeVisible(p.Text)) > maxTextLen {
				t.Fatal("over the length limit")
			}
			if strings.Contains(p.Text, "<script") || strings.Contains(p.Text, "<a ") {
				t.Fatalf("caller markup reached output: %q", p.Text)
			}
		}
	})
}
