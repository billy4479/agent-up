package main

import (
	"testing"
	"time"
)

func TestParseMaxUploadSize(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int64
		ok    bool
	}{
		{value: "", want: 0, ok: true},
		{value: "1048576", want: 1048576, ok: true},
		{value: "0"},
		{value: "-1"},
		{value: "1MB"},
	} {
		got, err := parseMaxUploadSize(test.value)
		if (err == nil) != test.ok || got != test.want {
			t.Fatalf("parseMaxUploadSize(%q) = %d, %v", test.value, got, err)
		}
	}
}

func TestParseUploadTTL(t *testing.T) {
	for _, test := range []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{value: "", want: 24 * time.Hour, ok: true},
		{value: "90m", want: 90 * time.Minute, ok: true},
		{value: "0"},
		{value: "-1h"},
		{value: "tomorrow"},
	} {
		got, err := parseUploadTTL(test.value)
		if (err == nil) != test.ok || got != test.want {
			t.Fatalf("parseUploadTTL(%q) = %s, %v", test.value, got, err)
		}
	}
}
