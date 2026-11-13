package main

import (
	"path/filepath"
	"testing"
)

func TestCheckUpdatePairAcceptsOnlyTheHelperNextToTheExe(t *testing.T) {
	dir := filepath.Join(`C:\`, "Programs", "easyTTY")
	exe := filepath.Join(dir, "easyTTY.exe")
	cases := []struct {
		name, self, target string
		ok                 bool
	}{
		{"helper next to exe", filepath.Join(dir, "easyTTY.new.exe"), exe, true},
		{"case differs", filepath.Join(dir, "EASYTTY.NEW.EXE"), exe, true},
		{"helper elsewhere", filepath.Join(`C:\`, "Temp", "easyTTY.new.exe"), exe, false},
		{"exe itself", exe, exe, false},
		{"relative target", "easyTTY.new.exe", "easyTTY.exe", false},
		{"not an exe", filepath.Join(dir, "sessions.new.exe"), filepath.Join(dir, "sessions.json"), false},
	}
	for _, c := range cases {
		err := checkUpdatePair(c.self, c.target)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}
