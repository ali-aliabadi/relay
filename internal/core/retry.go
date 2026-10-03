package core

import "time"

// MaxAttempts is how many times a delivery is tried before it fails.
const MaxAttempts = 5

// backoff[i] is the wait after the (i+1)th failed attempt.
var backoff = []time.Duration{10 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}

// NextAttempt returns when to retry after attempt (1-based) failed, and
// false when there are no attempts left. A provider's retryAfter wins when
// it is longer than the schedule.
func NextAttempt(now time.Time, attempt int, retryAfter time.Duration) (time.Time, bool) {
	if attempt >= MaxAttempts {
		return time.Time{}, false
	}
	wait := backoff[min(max(attempt, 1), len(backoff))-1]
	return now.Add(max(wait, retryAfter)), true
}
