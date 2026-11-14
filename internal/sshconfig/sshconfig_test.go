package sshconfig

import (
	"strings"
	"testing"
)

const sample = `
# personal machines
User nobody-default-is-overridden-below

Host web-1 web-1-alias
    HostName 192.0.2.10
    User ubuntu
    IdentityFile ~/.ssh/cloud_key

Host nas
  HostName=nas.example.internal
  Port 2222

Host pi
    HostName "198.51.100.4"
    User pi
    ProxyJump web-1

Host bastion-*
    User ops

Host *.example.com
    User admin

Host *
    User fallback
    IdentityFile ~/.ssh/id_ed25519
    Port 2200

Match host secret
    User ignored

Include ~/.ssh/config.d/*
`

func TestParse(t *testing.T) {
	r := Parse(sample)
	got := map[string]Entry{}
	var names []string
	for _, e := range r.Entries {
		got[e.Name] = e
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "web-1,web-1-alias,nas,pi" {
		t.Fatalf("names = %v", names)
	}
	// options before the first Host apply to every host and win (first value wins)
	if e := got["web-1"]; e.Host != "192.0.2.10" || e.User != "nobody-default-is-overridden-below" || e.Port != 2200 {
		t.Errorf("web-1 = %+v", e)
	}
	if e := got["nas"]; e.Host != "nas.example.internal" || e.Port != 2222 {
		t.Errorf("nas = %+v", e)
	}
	if e := got["pi"]; e.Host != "198.51.100.4" || e.ProxyJmp != "web-1" {
		t.Errorf("pi = %+v", e)
	}
	if len(r.Skipped) != 2 {
		t.Errorf("skipped = %v", r.Skipped)
	}
}

func TestFirstValueWinsAndDefaults(t *testing.T) {
	r := Parse("Host a\n  User one\n  User two\nHost *\n  User star\nHost b\n")
	if len(r.Entries) != 2 || r.Entries[0].User != "one" || r.Entries[1].User != "star" || r.Entries[1].Host != "b" {
		t.Errorf("entries = %+v", r.Entries)
	}
}

func TestMatches(t *testing.T) {
	for _, c := range []struct {
		pats []string
		host string
		want bool
	}{
		{[]string{"*"}, "x", true},
		{[]string{"web-?"}, "web-1", true},
		{[]string{"web-?"}, "web-10", false},
		{[]string{"*", "!secret"}, "secret", false},
		{[]string{"*.example.com"}, "a.example.com", true},
	} {
		if matches(c.pats, c.host) != c.want {
			t.Errorf("matches(%v, %s) != %v", c.pats, c.host, c.want)
		}
	}
}
