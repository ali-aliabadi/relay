package core

import (
	"errors"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/channel/fake"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

type multipartText struct{ *fake.Channel }

func (m multipartText) Preview(message.Message) (channel.Preview, error) {
	return channel.Preview{Parts: []channel.Part{{Kind: "text"}, {Kind: "text"}}}, nil
}

func TestDeletePreflightRejectsMultipartBeforeAnyProviderCall(t *testing.T) {
	valid := fake.New("first")
	multipart := multipartText{fake.New("second")}
	svc := &Messages{}
	_, err := svc.deleteTargets(store.Message{}, []message.Block{{Type: message.BlockText, Text: "text"}}, []editTarget{{editor: valid}, {editor: multipart}})
	if !errors.Is(err, ErrNotDeletable) || len(valid.Deleted()) != 0 || len(multipart.Deleted()) != 0 {
		t.Fatal("multipart preflight failed")
	}
}
