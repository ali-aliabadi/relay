package main

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/message"
)

// TestFileThroughServe sends the largest file the default body limit allows,
// and with a raised RELAY_MAX_BODY_BYTES the largest allowed file, through the
// public API, and checks each reaches Telegram as a document without the
// file's name or bytes reaching the logs.
func TestFileThroughServe(t *testing.T) {
	t.Run("default body limit", func(t *testing.T) { fileThroughServe(t, message.DefaultFileBytes, "") })
	t.Run("raised body limit", func(t *testing.T) { fileThroughServe(t, message.MaxFileBytes, "67108864") })
}

func fileThroughServe(t *testing.T, size int, maxBody string) {
	t.Helper()
	c, bot := newTelegramCLI(t)
	if maxBody != "" {
		env := c.env
		c.env = func(k string) (string, bool) {
			if k == "RELAY_MAX_BODY_BYTES" {
				return maxBody, true
			}
			return env(k)
		}
	}
	c.run("recipients", "add", "ali", "--name", "Ali")
	base, logs := startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 515151)); rc != 0 {
		t.Fatal(errOut)
	}
	_, out, _ := c.run("clients", "create", "billing")
	api := &apiClient{t: t, base: base, key: keyRe.FindString(out)}

	data := bytes.Repeat([]byte("secret-invoice-bytes "), size/21)
	data = append(data, make([]byte, size-len(data))...)
	created := api.call("POST", "/v1/messages", `{"to":["ali"],"title":"Invoice","blocks":[
		{"type":"file","filename":"private-invoice.pdf","content_type":"application/pdf",
		 "caption":"September","base64":"`+base64.StdEncoding.EncodeToString(data)+`"}]}`)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create = %s", api.lastBody)
	}
	waitUpTo(t, time.Minute, func() bool {
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
