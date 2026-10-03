package core

import "github.com/ali-aliabadi/relay/internal/message"

// PlanInput is everything routing looks at for one recipient.
type PlanInput struct {
	Urgency    string
	Preference []string        // the recipient's channel_preference, in order
	Override   []string        // the request's channels, if any
	Linked     map[string]bool // channels the recipient has a contact on
	Configured map[string]bool // channels this Relay instance can send on
}

// Plan returns the channels to create deliveries on now, in order.
//
// Candidates are the override (or else the preference), keeping only
// channels that are configured and linked. critical goes to every candidate
// at once; every other urgency goes to the first. Fallback to the next
// candidate after failures is the worker's job.
func Plan(in PlanInput) []string {
	candidates := in.Preference
	if len(in.Override) > 0 {
		candidates = in.Override
	}
	var usable []string
	for _, ch := range candidates {
		if in.Configured[ch] && in.Linked[ch] {
			usable = append(usable, ch)
		}
	}
	if len(usable) == 0 {
		return nil
	}
	if in.Urgency == message.UrgencyCritical {
		return usable
	}
	return usable[:1]
}
