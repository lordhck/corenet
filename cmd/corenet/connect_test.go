package main

import (
	"strings"
	"testing"
)

func TestDropInContent(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:53":    "DNS=127.0.0.1\n",
		"127.0.0.1:15353": "DNS=127.0.0.1:15353\n",
		"0.0.0.0:53":      "DNS=127.0.0.1\n",
		"0.0.0.0:15353":   "DNS=127.0.0.1:15353\n",
	}
	for addr, want := range cases {
		got := dropInContent(addr)
		if !strings.Contains(got, want) {
			t.Errorf("%s: content = %q, want it to contain %q", addr, got, want)
		}
		// Only the .core domain may ever be routed to corenetd.
		if !strings.Contains(got, "Domains=~core\n") {
			t.Errorf("%s: content = %q, want Domains=~core", addr, got)
		}
		if !strings.HasPrefix(got, "# Managed by corenet") {
			t.Errorf("%s: the file should say who wrote it: %q", addr, got)
		}
	}
}

func TestParseArgsAcceptsOptionsAnywhere(t *testing.T) {
	cases := []struct {
		args     []string
		wantArgs []string
		wantJSON bool
	}{
		{[]string{"service", "list"}, []string{"service", "list"}, false},
		{[]string{"--json", "service", "list"}, []string{"service", "list"}, true},
		{[]string{"service", "list", "--json"}, []string{"service", "list"}, true},
		{[]string{"service", "add", "a.core", "127.0.0.1", "80"}, []string{"service", "add", "a.core", "127.0.0.1", "80"}, false},
	}
	for _, c := range cases {
		var opts options
		got, err := parseArgs(&opts, c.args)
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if strings.Join(got, " ") != strings.Join(c.wantArgs, " ") || opts.json != c.wantJSON {
			t.Errorf("%v: args = %v, json = %v", c.args, got, opts.json)
		}
	}
}

func TestParseArgsReadsTheSocketOption(t *testing.T) {
	var opts options
	if _, err := parseArgs(&opts, []string{"status", "--socket", "/tmp/x.sock"}); err != nil {
		t.Fatal(err)
	}
	if opts.socket != "/tmp/x.sock" {
		t.Errorf("socket = %q", opts.socket)
	}
}
