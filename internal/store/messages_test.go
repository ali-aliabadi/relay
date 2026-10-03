package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

const (
	secretTitle   = "Backup failed on nas"
	secretBlocks  = `[{"type":"text","text":"disk sda is 98% full"}]`
	secretAddress = "987654321"
)

var secretImage = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("private-pixels"), 10)...)

type fixture struct {
	s         *Store
	conn      *sql.DB
	clientID  string
	otherID   string
	recipient Recipient
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	s, conn := newTestStore(t)
	ctx := t.Context()
	c1, err := s.CreateClient(ctx, "app-one", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.CreateClient(ctx, "app-two", bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRecipient(ctx, Recipient{Username: "ali", DisplayName: "Ali"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertContact(ctx, Contact{RecipientID: r.ID, Channel: "telegram", Address: secretAddress}); err != nil {
		t.Fatal(err)
	}
	return fixture{s: s, conn: conn, clientID: c1.ID, otherID: c2.ID, recipient: r}
}

func (f fixture) create(t *testing.T, key string) Message {
	t.Helper()
	m, err := f.s.CreateMessage(t.Context(), NewMessage{
		Message: Message{
			ClientID: f.clientID, Urgency: "high", Title: secretTitle, Blocks: []byte(secretBlocks),
			Source: "backup-script", IdempotencyKey: key, RequestID: "req_1",
		},
		Attachments: []Attachment{{ContentType: "image/png", Bytes: secretImage}},
		Deliveries:  []PlannedDelivery{{RecipientID: f.recipient.ID, Channel: "telegram"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCreateAndReadMessage(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "k1")
	if m.Status != StatusQueued || m.ID == "" {
		t.Errorf("created = %+v", m)
	}

	got, err := f.s.Message(ctx, f.clientID, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != secretTitle || string(got.Blocks) != secretBlocks || got.Source != "backup-script" ||
		got.IdempotencyKey != "k1" || got.RequestID != "req_1" || got.Urgency != "high" || !got.CreatedAt.Equal(m.CreatedAt) {
		t.Errorf("read back = %+v", got)
	}
	if byKey, err := f.s.MessageByIdempotencyKey(ctx, f.clientID, "k1"); err != nil || byKey.ID != m.ID {
		t.Errorf("by idempotency key = %+v, %v", byKey, err)
	}

	atts, err := f.s.Attachments(ctx, m.ID)
	if err != nil || len(atts) != 1 || !bytes.Equal(atts[0].Bytes, secretImage) || atts[0].Size != int64(len(secretImage)) {
		t.Errorf("attachments = %v, %v", atts, err)
	}
	ds, err := f.s.Deliveries(ctx, m.ID)
	if err != nil || len(ds) != 1 || ds[0].Status != StatusQueued || ds[0].Channel != "telegram" ||
		ds[0].RecipientID != f.recipient.ID || !ds[0].NextAttemptAt.Equal(m.CreatedAt) {
		t.Errorf("deliveries = %+v, %v", ds, err)
	}
}

func TestMessageWithoutTitle(t *testing.T) {
	f := newFixture(t)
	m, err := f.s.CreateMessage(t.Context(), NewMessage{Message: Message{
		ClientID: f.clientID, Urgency: "normal", Blocks: []byte(`[]`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	var title []byte
	if err := f.conn.QueryRowContext(t.Context(), "SELECT title FROM messages WHERE id = ?", m.ID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != nil {
		t.Errorf("absent title stored as %x, want NULL", title)
	}
	got, err := f.s.Message(t.Context(), f.clientID, m.ID)
	if err != nil || got.Title != "" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestMessagesAreScopedToClient(t *testing.T) {
	f := newFixture(t)
	m := f.create(t, "k1")
	if _, err := f.s.Message(t.Context(), f.otherID, m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("other client read the message: %v", err)
	}
	if _, err := f.s.MessageByIdempotencyKey(t.Context(), f.otherID, "k1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other client found the key: %v", err)
	}
}

func TestIdempotencyConflictRollsBack(t *testing.T) {
	f := newFixture(t)
	f.create(t, "same")
	_, err := f.s.CreateMessage(t.Context(), NewMessage{
		Message:    Message{ClientID: f.clientID, Urgency: "low", Blocks: []byte(`[]`), IdempotencyKey: "same"},
		Deliveries: []PlannedDelivery{{RecipientID: f.recipient.ID, Channel: "telegram"}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	var n int
	if err := f.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM deliveries").Scan(&n); err != nil || n != 1 {
		t.Errorf("deliveries = %d, %v; the failed insert must leave nothing behind", n, err)
	}
	// A different client may reuse the key.
	if _, err := f.s.CreateMessage(t.Context(), NewMessage{
		Message: Message{ClientID: f.otherID, Urgency: "low", Blocks: []byte(`[]`), IdempotencyKey: "same"},
	}); err != nil {
		t.Errorf("other client with the same key: %v", err)
	}
}

func TestBadDeliveryRollsBackMessage(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.CreateMessage(t.Context(), NewMessage{
		Message:    Message{ClientID: f.clientID, Urgency: "low", Blocks: []byte(`[]`)},
		Deliveries: []PlannedDelivery{{RecipientID: "rcp_missing", Channel: "telegram"}},
	})
	if err == nil {
		t.Fatal("delivery to a missing recipient accepted")
	}
	var n int
	if err := f.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM messages").Scan(&n); err != nil || n != 0 {
		t.Errorf("messages = %d, %v; want the message rolled back", n, err)
	}
}

// TestRawRowsHoldNoPlaintext reads the database file's tables directly and
// checks that no private value appears in any column.
func TestRawRowsHoldNoPlaintext(t *testing.T) {
	f := newFixture(t)
	f.create(t, "k1")
	needles := [][]byte{[]byte(secretTitle), []byte("disk sda"), []byte(secretAddress), []byte("private-pixels")}
	for _, table := range []string{"clients", "recipients", "contacts", "messages", "attachments", "deliveries"} {
		for col, raw := range rawValues(t, f.conn, table) {
			for _, n := range needles {
				if bytes.Contains(raw, n) {
					t.Errorf("%s.%s holds plaintext %q", table, col, n)
				}
			}
		}
	}
}

// rawValues returns every value in table as bytes, keyed by "column#row".
func rawValues(t *testing.T, conn *sql.DB, table string) map[string][]byte {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), "SELECT * FROM "+table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for n := 0; rows.Next(); n++ {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			out[fmt.Sprintf("%s#%d", cols[i], n)] = fmt.Appendf(nil, "%s", v)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSwappedCiphertextIsRejected moves one row's encrypted title onto another
// row; associated data must make the read fail rather than return the wrong content.
func TestSwappedCiphertextIsRejected(t *testing.T) {
	f := newFixture(t)
	a := f.create(t, "a")
	b := f.create(t, "b")
	if _, err := f.conn.ExecContext(t.Context(),
		"UPDATE messages SET title = (SELECT title FROM messages WHERE id = ?) WHERE id = ?", a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Message(t.Context(), f.clientID, b.ID); err == nil {
		t.Fatal("swapped ciphertext decrypted")
	}
}

func TestListMessages(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	var ids []string
	for i := range 5 {
		ids = append(ids, f.create(t, fmt.Sprintf("k%d", i)).ID)
	}
	if _, err := f.conn.ExecContext(ctx, "UPDATE messages SET status = 'delivered' WHERE id = ?", ids[1]); err != nil {
		t.Fatal(err)
	}

	page1, err := f.s.ListMessages(ctx, ListFilter{ClientID: f.clientID, Limit: 2})
	if err != nil || len(page1) != 2 || page1[0].ID != ids[4] || page1[1].ID != ids[3] {
		t.Fatalf("page1 = %v, %v", msgIDs(page1), err)
	}
	page2, err := f.s.ListMessages(ctx, ListFilter{ClientID: f.clientID, Limit: 2, BeforeID: page1[1].ID})
	if err != nil || len(page2) != 2 || page2[0].ID != ids[2] {
		t.Fatalf("page2 = %v, %v", msgIDs(page2), err)
	}
	delivered, err := f.s.ListMessages(ctx, ListFilter{ClientID: f.clientID, Status: StatusDelivered})
	if err != nil || len(delivered) != 1 || delivered[0].ID != ids[1] {
		t.Errorf("status filter = %v, %v", msgIDs(delivered), err)
	}
	m3, _ := f.s.Message(ctx, f.clientID, ids[3])
	since, err := f.s.ListMessages(ctx, ListFilter{ClientID: f.clientID, Since: m3.CreatedAt, Limit: 1000})
	if err != nil || len(since) != 2 {
		t.Errorf("since filter = %v, %v", msgIDs(since), err)
	}
	other, err := f.s.ListMessages(ctx, ListFilter{ClientID: f.otherID})
	if err != nil || len(other) != 0 {
		t.Errorf("other client sees %v, %v", msgIDs(other), err)
	}
}

func msgIDs(ms []Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}
