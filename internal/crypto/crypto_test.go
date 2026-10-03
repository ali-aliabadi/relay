package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, KeySize) }

func mustNew(t *testing.T, k []byte) *Cipher {
	t.Helper()
	c, err := New(k)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := mustNew(t, key(1))
	for _, plain := range [][]byte{nil, {}, []byte("x"), []byte("Backup failed on nas"), bytes.Repeat([]byte("a"), 1<<16)} {
		ct, err := c.Encrypt(plain, []byte("messages.title:msg_1"))
		if err != nil {
			t.Fatal(err)
		}
		if ct[0] != CurrentVersion {
			t.Errorf("version byte = %d", ct[0])
		}
		if len(plain) > 3 && bytes.Contains(ct, plain) {
			t.Error("ciphertext contains the plaintext")
		}
		got, err := c.Decrypt(ct, []byte("messages.title:msg_1"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, plain) {
			t.Errorf("round trip = %q, want %q", got, plain)
		}
	}
}

func TestNonceIsRandom(t *testing.T) {
	c := mustNew(t, key(1))
	a, _ := c.Encrypt([]byte("same"), nil)
	b, _ := c.Encrypt([]byte("same"), nil)
	if bytes.Equal(a, b) {
		t.Error("two encryptions of the same value are identical")
	}
}

func TestDecryptFailures(t *testing.T) {
	c := mustNew(t, key(1))
	aad := []byte("contacts.address:rcp_1")
	ct, err := c.Encrypt([]byte("123456789"), aad)
	if err != nil {
		t.Fatal(err)
	}
	flip := func(i int) []byte {
		b := bytes.Clone(ct)
		b[i] ^= 0x01
		return b
	}
	tests := []struct {
		name string
		c    *Cipher
		ct   []byte
		aad  []byte
	}{
		{"tampered body", c, flip(len(ct) - 1), aad},
		{"tampered nonce", c, flip(2), aad},
		{"unknown version", c, flip(0), aad},
		{"other row", c, ct, []byte("contacts.address:rcp_2")},
		{"wrong key", mustNew(t, key(2)), ct, aad},
		{"truncated", c, ct[:5], aad},
		{"empty", c, nil, aad},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.c.Decrypt(tt.ct, tt.aad); !errors.Is(err, ErrDecrypt) {
				t.Errorf("err = %v, want ErrDecrypt", err)
			}
		})
	}
}

func TestRotation(t *testing.T) {
	old := mustNew(t, key(1))
	ct, _ := old.Encrypt([]byte("old row"), nil)

	rotated, err := NewWithKeys(2, map[byte][]byte{1: key(1), 2: key(2)})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := rotated.Decrypt(ct, nil); err != nil || string(got) != "old row" {
		t.Errorf("decrypting v1 after rotation = %q, %v", got, err)
	}
	ct2, _ := rotated.Encrypt([]byte("new row"), nil)
	if ct2[0] != 2 {
		t.Errorf("new rows use version %d, want 2", ct2[0])
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	if _, err := New([]byte("short")); err == nil {
		t.Error("short key accepted")
	}
	if _, err := NewWithKeys(3, map[byte][]byte{1: key(1)}); err == nil {
		t.Error("missing current version accepted")
	}
}

func FuzzDecrypt(f *testing.F) {
	c, _ := New(key(1))
	ct, _ := c.Encrypt([]byte("seed"), nil)
	f.Add(ct)
	f.Add([]byte{})
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = c.Decrypt(data, nil) // must never panic
	})
}
