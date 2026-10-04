package telegram

import (
	"errors"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

func TestSendDocument(t *testing.T) {
	api, ch := newBotAPI(t)
	pdf := []byte("%PDF-1.7 private")
	id, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{
		Urgency: "normal", Title: "Invoice", DeliveryID: "dlv_1",
		Blocks: []message.Block{
			{Type: "file", Filename: `sep "final".pdf`, ContentType: "application/pdf", Caption: "Due soon", Attachment: idx(0)},
			{Type: "question", Text: "Paid?", Options: []string{"Yes"}},
		},
		Attachments: []message.Attachment{{ContentType: "application/pdf", Bytes: pdf}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 1 || id != "11" {
		t.Fatalf("calls = %d, id = %s", len(api.calls), id)
	}
	c := api.calls[0]
	if c.Method != "sendDocument" || string(c.FileBytes) != string(pdf) || c.FileName != `sep "final".pdf` ||
		c.FileType != "application/pdf" || c.Form["chat_id"] != "42" || c.Form["parse_mode"] != "HTML" {
		t.Errorf("document call = %+v", c)
	}
	if !strings.Contains(c.Form["caption"], "<i>Due soon</i>") || !strings.Contains(c.Form["reply_markup"], "a:dlv_1:0") {
		t.Errorf("caption or buttons missing: %+v", c.Form)
	}
}

func TestSendPhotoFileAndText(t *testing.T) {
	api, ch := newBotAPI(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), 1)
	id, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{
		Urgency: "normal",
		Blocks: []message.Block{
			{Type: "image", Attachment: idx(0)},
			{Type: "file", Filename: "data.csv", ContentType: "text/csv", Attachment: idx(1)},
			{Type: "text", Text: "Numbers attached."},
		},
		Attachments: []message.Attachment{{ContentType: "image/png", Bytes: png}, {ContentType: "text/csv", Bytes: []byte("a,b\n1,2\n")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var methods []string
	for _, c := range api.calls {
		methods = append(methods, c.Method)
	}
	if strings.Join(methods, ",") != "sendPhoto,sendDocument,sendMessage" || id != "13" { // the last part
		t.Fatalf("methods = %v, id = %s", methods, id)
	}
	if api.calls[0].FileName != "image.png" || string(api.calls[1].FileBytes) != "a,b\n1,2\n" || api.calls[1].FileName != "data.csv" {
		t.Errorf("uploads = %+v / %+v", api.calls[0], api.calls[1])
	}
}

func TestMissingInlineFileIsPermanent(t *testing.T) {
	_, ch := newBotAPI(t)
	_, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{Blocks: []message.Block{
		{Type: "file", Filename: "a.txt", Attachment: idx(0)},
	}})
	var ce *channel.Error
	if !errors.As(err, &ce) || !ce.Permanent || strings.Contains(ce.Reason, "a.txt") {
		t.Fatalf("err = %v", err)
	}
}

func TestMultipartEscapesFilename(t *testing.T) {
	api, ch := newBotAPI(t)
	if _, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{
		Blocks:      []message.Block{{Type: "file", Filename: "a\r\nX-Evil: 1\".txt", Attachment: idx(0)}},
		Attachments: []message.Attachment{{ContentType: "text/plain", Bytes: []byte("x")}},
	}); err != nil {
		t.Fatal(err)
	}
	if c := api.calls[0]; c.FileName != `aX-Evil: 1".txt` || string(c.FileBytes) != "x" {
		t.Errorf("call = %+v", c)
	}
}
