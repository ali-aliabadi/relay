package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

const (
	colAnswer = "answers.answer"
	colAudio  = "answers.audio"
)

func answerRowID(messageID, recipientID string) string { return messageID + "/" + recipientID }

// Answer is one recipient's answer to a message's question. Text and Audio
// are private. A voice answer has Audio and no Text.
type Answer struct {
	MessageID   string
	RecipientID string
	Username    string
	Text        string
	Audio       []byte
	AudioType   string
	AnsweredAt  time.Time
}

// LogValue keeps the answer text out of logs.
func (a Answer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("message_id", a.MessageID), slog.String("recipient_id", a.RecipientID))
}

// SaveAnswer stores or replaces a recipient's answer. It returns false when
// the app has already fetched the answer, which makes it final.
func (s *Store) SaveAnswer(ctx context.Context, messageID, recipientID, text string) (bool, error) {
	return s.saveAnswer(ctx, messageID, recipientID, text, nil, "")
}

// SaveVoiceAnswer is SaveAnswer for a voice note: audio of contentType.
func (s *Store) SaveVoiceAnswer(ctx context.Context, messageID, recipientID, contentType string, audio []byte) (bool, error) {
	return s.saveAnswer(ctx, messageID, recipientID, "", audio, contentType)
}

func (s *Store) saveAnswer(ctx context.Context, messageID, recipientID, text string, audio []byte, audioType string) (bool, error) {
	rowID := answerRowID(messageID, recipientID)
	sealed, err := s.seal([]byte(text), colAnswer, rowID)
	if err != nil {
		return false, err
	}
	sealedAudio, err := s.seal(audio, colAudio, rowID) // nil for a text answer
	if err != nil {
		return false, err
	}
	n, err := s.q.SaveAnswer(ctx, db.SaveAnswerParams{
		MessageID: messageID, RecipientID: recipientID, Answer: sealed, Audio: sealedAudio,
		AudioType: nullString(audioType), AnsweredAt: formatTime(s.clock()),
	})
	if err != nil {
		return false, fmt.Errorf("saving answer: %w", mapErr(err))
	}
	return n > 0, nil
}

// TakeAnswers returns a message's answers and marks them fetched, so later
// replies can no longer change them.
func (s *Store) TakeAnswers(ctx context.Context, messageID string) ([]Answer, error) {
	var out []Answer
	err := s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListAnswersForMessage(ctx, messageID)
		if err != nil {
			return fmt.Errorf("listing answers: %w", err)
		}
		out = make([]Answer, 0, len(rows))
		for _, r := range rows {
			rowID := answerRowID(r.MessageID, r.RecipientID)
			text, err := s.open(r.Answer, colAnswer, rowID)
			if err != nil {
				return err
			}
			audio, err := s.open(r.Audio, colAudio, rowID) // nil for a text answer
			if err != nil {
				return err
			}
			at, err := parseTime(r.AnsweredAt)
			if err != nil {
				return err
			}
			out = append(out, Answer{
				MessageID: r.MessageID, RecipientID: r.RecipientID, Username: r.Username, Text: string(text),
				Audio: audio, AudioType: r.AudioType.String, AnsweredAt: at,
			})
		}
		if err := q.MarkAnswersFetched(ctx, db.MarkAnswersFetchedParams{
			FetchedAt: nullString(formatTime(s.clock())), MessageID: messageID,
		}); err != nil {
			return fmt.Errorf("marking answers fetched: %w", err)
		}
		return nil
	})
	return out, err
}

// Delivery returns one delivery by ID.
func (s *Store) Delivery(ctx context.Context, deliveryID string) (Delivery, error) {
	row, err := s.q.GetDelivery(ctx, deliveryID)
	if err != nil {
		return Delivery{}, fmt.Errorf("getting delivery: %w", mapErr(err))
	}
	return deliveryFromRow(row)
}

// DeliveriesByProviderID returns the deliveries a provider message ID belongs
// to. Telegram IDs are per chat, so there can be several.
func (s *Store) DeliveriesByProviderID(ctx context.Context, channel, providerID string) ([]Delivery, error) {
	rows, err := s.q.ListDeliveriesByProviderID(ctx, db.ListDeliveriesByProviderIDParams{
		Channel: channel, ProviderMessageID: nullString(providerID),
	})
	if err != nil {
		return nil, fmt.Errorf("listing deliveries by provider id: %w", err)
	}
	out := make([]Delivery, 0, len(rows))
	for _, r := range rows {
		d, err := deliveryFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}
