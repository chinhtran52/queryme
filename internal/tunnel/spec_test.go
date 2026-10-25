package tunnel

import (
	"os/user"
	"testing"
)

func TestParseSpec(t *testing.T) {
	me, _ := user.Current()
	cases := map[string][2]string{
		"bob@bastion":       {"bob", "bastion:22"},
		"bob@bastion:2222":  {"bob", "bastion:2222"},
		"bastion":           {me.Username, "bastion:22"},
		"a@b@bastion":       {"a@b", "bastion:22"},
		"bob@[::1]":         {"bob", "[::1]:22"},
		"bob@[::1]:2200":    {"bob", "[::1]:2200"},
		" bob@bastion.lan ": {"bob", "bastion.lan:22"},
	}
	for spec, want := range cases {
		username, addr, err := parseSpec(spec)
		if err != nil || username != want[0] || addr != want[1] {
			t.Errorf("parseSpec(%q) = %q, %q, %v", spec, username, addr, err)
		}
	}
	for _, bad := range []string{"", "bob@"} {
		if _, _, err := parseSpec(bad); err == nil {
			t.Errorf("parseSpec(%q) should fail", bad)
		}
	}
}
