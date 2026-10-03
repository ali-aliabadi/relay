package core

import (
	"slices"
	"testing"
)

func TestPlan(t *testing.T) {
	all := map[string]bool{"telegram": true, "sms": true, "email": true}
	tests := []struct {
		name string
		in   PlanInput
		want []string
	}{
		{"normal takes first preference", PlanInput{Urgency: "normal", Preference: []string{"sms", "telegram"}, Linked: all, Configured: all}, []string{"sms"}},
		{"low takes first", PlanInput{Urgency: "low", Preference: []string{"telegram", "sms"}, Linked: all, Configured: all}, []string{"telegram"}},
		{"high takes first (fallback is the worker's)", PlanInput{Urgency: "high", Preference: []string{"telegram", "sms"}, Linked: all, Configured: all}, []string{"telegram"}},
		{"critical takes all", PlanInput{Urgency: "critical", Preference: []string{"telegram", "sms", "email"}, Linked: all, Configured: all}, []string{"telegram", "sms", "email"}},
		{"override wins", PlanInput{Urgency: "normal", Preference: []string{"telegram"}, Override: []string{"email"}, Linked: all, Configured: all}, []string{"email"}},
		{"skips unconfigured", PlanInput{Urgency: "normal", Preference: []string{"sms", "telegram"}, Linked: all, Configured: map[string]bool{"telegram": true}}, []string{"telegram"}},
		{"skips unlinked", PlanInput{Urgency: "critical", Preference: []string{"sms", "telegram"}, Linked: map[string]bool{"telegram": true}, Configured: all}, []string{"telegram"}},
		{"nothing usable", PlanInput{Urgency: "normal", Preference: []string{"sms"}, Linked: map[string]bool{"telegram": true}, Configured: all}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Plan(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("Plan = %v, want %v", got, tt.want)
			}
		})
	}
}
