package message

import (
	"encoding/base64"
	"strings"
	"testing"
)

func fileB64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestNormalizeFile(t *testing.T) {
	req := Request{To: []string{"ali"}, Blocks: []Block{
		{Type: BlockText, Text: "Monthly report attached."},
		{Type: BlockFile, Filename: "report 2026-09.pdf", ContentType: "Application/PDF", Base64: fileB64("%PDF-1.7"), Caption: "September"},
		{Type: BlockImage, Base64: pngB64, ContentType: "image/png"},
	}}
	got, atts, err := Normalize(req)
	if err != nil {
		t.Fatal(err)
	}
	f := got.Blocks[1]
	if len(atts) != 2 || f.Attachment == nil || string(atts[*f.Attachment].Bytes) != "%PDF-1.7" {
		t.Fatalf("attachments = %+v, file block = %+v", atts, f)
	}
	if f.Base64 != "" || f.ContentType != "application/pdf" || atts[*f.Attachment].ContentType != "application/pdf" {
		t.Errorf("file block not rewritten: %+v", f)
	}
	if f.Filename != "report 2026-09.pdf" || f.Caption != "September" {
		t.Errorf("file block lost its name or caption: %+v", f)
	}
	if req.Blocks[1].Base64 == "" {
		t.Error("Normalize mutated the caller's blocks")
	}

	got, atts, err = Normalize(Request{To: []string{"ali"}, Blocks: []Block{
		{Type: BlockFile, Filename: "گزارش.txt", Base64: fileB64("hi")},
	}})
	if err != nil || got.Blocks[0].ContentType != DefaultFileContentType || atts[0].ContentType != DefaultFileContentType {
		t.Errorf("default content type: %+v, %v", got.Blocks, err)
	}
}

func TestNormalizeRejectsFiles(t *testing.T) {
	ok := fileB64("data")
	big := base64.StdEncoding.EncodeToString(make([]byte, MaxFileBytes+1))
	tests := []struct {
		name string
		b    Block
		want string
	}{
		{"no filename", Block{Type: BlockFile, Base64: ok}, "filename: required"},
		{"long filename", Block{Type: BlockFile, Filename: strings.Repeat("a", MaxFilenameLen+1), Base64: ok}, "filename: longer than"},
		{"path", Block{Type: BlockFile, Filename: "../etc/passwd", Base64: ok}, "plain file name"},
		{"backslash", Block{Type: BlockFile, Filename: `a\b.txt`, Base64: ok}, "plain file name"},
		{"dots", Block{Type: BlockFile, Filename: "..", Base64: ok}, "plain file name"},
		{"newline", Block{Type: BlockFile, Filename: "a\r\nContent-Type: x", Base64: ok}, "plain file name"},
		{"no bytes", Block{Type: BlockFile, Filename: "a.txt"}, "base64: required"},
		{"bad base64", Block{Type: BlockFile, Filename: "a.txt", Base64: "!!!"}, "not valid standard base64"},
		{"too big", Block{Type: BlockFile, Filename: "a.bin", Base64: big}, "file larger than"},
		{"bad type", Block{Type: BlockFile, Filename: "a.txt", Base64: ok, ContentType: "pdf"}, "content_type: must be a media type"},
		{"header injection", Block{Type: BlockFile, Filename: "a.txt", Base64: ok, ContentType: "text/plain\r\nX: y"}, "content_type"},
		{"url", Block{Type: BlockFile, Filename: "a.txt", Base64: ok, URL: "https://e.x/a.txt"}, "don't belong"},
		{"long caption", Block{Type: BlockFile, Filename: "a.txt", Base64: ok, Caption: strings.Repeat("c", MaxCaptionLen+1)}, "caption: longer than"},
		{"filename on image", Block{Type: BlockImage, URL: "https://e.x/a.png", Filename: "a.png"}, "don't belong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Normalize(Request{To: []string{"ali"}, Blocks: []Block{tt.b}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "passwd") || strings.Contains(err.Error(), "Content-Type: x") {
				t.Errorf("error echoes input: %v", err)
			}
		})
	}

	if _, _, err := Normalize(Request{To: []string{"ali"}, Blocks: []Block{
		{Type: BlockFile, Filename: "a.txt", Base64: ok},
		{Type: BlockFile, Filename: "b.txt", Base64: ok},
	}}); err == nil || !strings.Contains(err.Error(), "at most 1 file") {
		t.Errorf("two files: %v", err)
	}
}
