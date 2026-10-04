package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/message"
)

// TestFileThroughServe sends the largest allowed file through the public API
// and checks it reaches Telegram as a document, within the default body limit,
// without the file's name or bytes reaching the logs.
func TestFileThroughServe(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	base, logs := startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 515151)); rc != 0 {
		t.Fatal(errOut)
	}
	_, out, _ := c.run("clients", "create", "billing")
	api := &apiClient{t: t, base: base, key: keyRe.FindString(out)}

	data := bytes.Repeat([]byte("secret-invoice-bytes "), message.MaxFileBytes/21)
	data = append(data, make([]byte, message.MaxFileBytes-len(data))...)
	created := api.call("POST", "/v1/messages", `{"to":["ali"],"title":"Invoice","blocks":[
		{"type":"file","filename":"private-invoice.pdf","content_type":"application/pdf",
		 "caption":"September","base64":"`+base64.StdEncoding.EncodeToString(data)+`"}]}`)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create = %s", api.lastBody)
	}
	waitFor(t, func() bool {
		return api.call("GET", "/v1/messages/"+id, "")["status"] == "delivered"
	})
	docs := bot.Calls("sendDocument")
	if len(docs) != 1 || docs[0].File != len(data) || docs[0].Form["chat_id"] != "515151" ||
		!strings.Contains(docs[0].Form["caption"], "September") {
		t.Fatalf("sendDocument calls = %+v", docs)
	}
	for _, leak := range []string{"private-invoice", "secret-invoice", "September"} {
		if strings.Contains(logs.String(), leak) {
			t.Errorf("logs contain %q", leak)
		}
	}
}
