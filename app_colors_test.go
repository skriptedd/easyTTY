package main

import (
	"testing"

	"easytty/internal/store"
)

func TestAutoColorGroupsByNamePrefix(t *testing.T) {
	existing := []store.Session{{Name: "core-sw1", Color: "#ff5d6c"}}
	in := []store.Session{
		{Name: "con1-cda-r12"}, {Name: "con2-cda-r13"},
		{Name: "is01-ntr-000"}, {Name: "is02-ntr-000 P3"},
		{Name: "core-sw2"}, {Name: "10.0.0.1"}, {Name: "fw-01", Color: "#8a94a6"},
	}
	out := autoColor(existing, in)
	if out[0].Color == "" || out[0].Color != out[1].Color {
		t.Errorf("con group: %q %q", out[0].Color, out[1].Color)
	}
	if out[2].Color == "" || out[2].Color != out[3].Color || out[2].Color == out[0].Color {
		t.Errorf("is group: %q %q (con %q)", out[2].Color, out[3].Color, out[0].Color)
	}
	if out[4].Color != "#ff5d6c" {
		t.Errorf("existing group colour not reused: %q", out[4].Color)
	}
	if out[0].Color == "#ff5d6c" || out[2].Color == "#ff5d6c" {
		t.Error("new groups must not take a colour already in use")
	}
	if out[5].Color != "" || out[6].Color != "#8a94a6" {
		t.Errorf("no prefix / own colour: %q %q", out[5].Color, out[6].Color)
	}
	if in[0].Color != "" {
		t.Error("input must not be modified")
	}
}
