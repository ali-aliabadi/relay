// Package id makes prefixed ULIDs such as "msg_01J9ZK3Q4R8YB6X5V2N7T0M1CD".
package id

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// Prefixes for every entity type.
const (
	Client     = "cli"
	Recipient  = "rcp"
	Message    = "msg"
	Delivery   = "dlv"
	Attachment = "att"
)

// New returns prefix + "_" + a ULID for time t. IDs sort by creation time.
func New(prefix string, t time.Time) string {
	return prefix + "_" + ulid.MustNew(ulid.Timestamp(t), rand.Reader).String()
}
