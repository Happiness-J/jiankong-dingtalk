package main

import "testing"

func TestHuman(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{8 * 1024 * 1024 * 1024, "8.0 GB"},
	}
	for _, c := range cases {
		if got := human(c.in); got != c.want {
			t.Errorf("human(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIcon(t *testing.T) {
	if icon(50, 80) != "✅" {
		t.Error("icon below threshold should be ✅")
	}
	if icon(80, 80) != "✅" {
		t.Error("icon at threshold should be ✅")
	}
	if icon(81, 80) != "⚠️" {
		t.Error("icon above threshold should be ⚠️")
	}
}

func TestSignDing(t *testing.T) {
	a := signDing("SECtest", 1700000000000)
	b := signDing("SECtest", 1700000000000)
	if a == "" {
		t.Fatal("signDing returned empty")
	}
	if a != b {
		t.Error("signDing should be deterministic for same input")
	}
	if c := signDing("SECtest", 1700000000001); c == a {
		t.Error("signDing should change with timestamp")
	}
}
