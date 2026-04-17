package normalize

import "testing"

func TestNonEmpty(t *testing.T) {
	t.Run("trimmed value", func(t *testing.T) {
		got, ok := NonEmpty("  hello  ")
		if !ok || got != "hello" {
			t.Fatalf("expected hello,true got %q,%v", got, ok)
		}
	})
	t.Run("empty after trim", func(t *testing.T) {
		got, ok := NonEmpty("  \n\t  ")
		if ok || got != "" {
			t.Fatalf("expected empty,false got %q,%v", got, ok)
		}
	})
}

func TestDomainOrHost(t *testing.T) {
	cases := []struct {
		in   string
		out  string
		okay bool
	}{
		{in: "", out: "", okay: true},
		{in: "https://Example.COM/login", out: "example.com", okay: true},
		{in: "company.ghe.com", out: "company.ghe.com", okay: true},
		{in: " bad/path ", out: "", okay: false},
	}
	for _, tc := range cases {
		got, ok := DomainOrHost(tc.in)
		if got != tc.out || ok != tc.okay {
			t.Fatalf("DomainOrHost(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.out, tc.okay)
		}
	}
}

func TestShortcutKey(t *testing.T) {
	if got := ShortcutKey(" :Help "); got != "help" {
		t.Fatalf("expected help, got %q", got)
	}
	if got := ShortcutKey("foo bar"); got != "" {
		t.Fatalf("expected empty for whitespace-containing shortcut, got %q", got)
	}
}
