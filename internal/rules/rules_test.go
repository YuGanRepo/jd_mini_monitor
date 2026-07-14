package rules

import (
	"net/http"
	"testing"
)

func TestMatchPriorityAndDeclarationOrder(t *testing.T) {
	enabled := true
	set, err := NewSet([]Rule{
		{
			Name:       "lower priority",
			Enabled:    &enabled,
			Priority:   1,
			Host:       "api.example.test",
			PathPrefix: "/v1",
			Action:     Action{Type: "mock", Body: "lower"},
		},
		{
			Name:       "higher priority",
			Enabled:    &enabled,
			Priority:   10,
			HostSuffix: "example.test",
			PathPrefix: "/v1",
			Action:     Action{Type: "mock", Body: "higher"},
		},
	}, ".")
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	request, err := http.NewRequest(http.MethodGet, "https://api.example.test/v1/users", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	matchedRule, ok := set.Match(request)
	if !ok {
		t.Fatal("expected a matching rule")
	}
	if matchedRule.Name != "higher priority" {
		t.Fatalf("matched rule = %q, want higher priority", matchedRule.Name)
	}
}

func TestDisabledRuleDoesNotMatch(t *testing.T) {
	disabled := false
	set, err := NewSet([]Rule{
		{
			Name:     "disabled",
			Enabled:  &disabled,
			Host:     "api.example.test",
			Path:     "/v1/users",
			Action:   Action{Type: "mock", Body: "disabled"},
		},
	}, ".")
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	request, err := http.NewRequest(http.MethodGet, "https://api.example.test/v1/users", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	if _, ok := set.Match(request); ok {
		t.Fatal("disabled rule matched unexpectedly")
	}
}

func TestMethodPathRegexQueryAndHeaders(t *testing.T) {
	set, err := NewSet([]Rule{
		{
			Name:      "specific post",
			HostGlob:  "*.example.test",
			Method:    http.MethodPost,
			PathRegex: `^/v[0-9]+/orders$`,
			Query:     map[string]string{"mode": "test"},
			Headers:   map[string]string{"X-Trace": "enabled"},
			Action:    Action{Type: "mock", Body: "ok"},
		},
	}, ".")
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, "https://shop.example.test/v2/orders?mode=test", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("X-Trace", "enabled")

	if _, ok := set.Match(request); !ok {
		t.Fatal("expected specific rule to match")
	}
}

func TestInvalidRuleReportsField(t *testing.T) {
	_, err := NewSet([]Rule{
		{
			Name:      "broken",
			Host:      "api.example.test",
			PathRegex: "(",
			Action:    Action{Type: "mock"},
		},
	}, ".")
	if err == nil {
		t.Fatal("expected invalid pathRegex error")
	}
}
