// Package id makes prefixed ULIDs such as "msg_01J9ZK3Q4R8YB6X5V2N7T0M1CD".
package id

import (
	"crypto/rand"
	"sync"
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

var (
	mu sync.Mutex
	// Monotonic entropy keeps IDs made in the same millisecond in creation
	// order, which list pagination relies on.
	entropy = ulid.Monotonic(rand.Reader, 0)
)

// New returns prefix + "_" + a ULID for time t. IDs sort by creation time,
// including several made within one millisecond.
func New(prefix string, t time.Time) string {
	mu.Lock()
	defer mu.Unlock()
	return prefix + "_" + ulid.MustNew(ulid.Timestamp(t), entropy).String()
}
