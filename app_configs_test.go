package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easytty/internal/automation"
)

func TestConfigFileName(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 5, 7, 0, time.Local)
	for title, want := range map[string]string{
		"Core-sw1":            "Core-sw1_2026-11-11_090507.cfg",
		"Switch konsole/COM6": "Switch_konsole_COM6_2026-11-11_090507.cfg",
		`..\..\boot`:          "boot_2026-11-11_090507.cfg",
		"":                    "config_2026-11-11_090507.cfg",
	} {
		if got := outputFileName(title, "", at, ".cfg"); got != want {
			t.Errorf("configFileName(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestUniqueFileKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sw1.cfg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := filepath.Base(uniqueFile(dir, "sw1.cfg")); got != "sw1 (1).cfg" {
		t.Errorf("got %q", got)
	}
	if got := filepath.Base(uniqueFile(dir, "neu.cfg")); got != "neu.cfg" {
		t.Errorf("got %q", got)
	}
}

func TestRunningConfigText(t *testing.T) {
	ok := automation.Result{Steps: []automation.Step{{Command: showRunning, Output: "Building configuration...\n\nhostname sw1\nend\n"}}}
	if got, err := runningConfigText(ok); err != nil || !strings.HasPrefix(got, "Building") || !strings.HasSuffix(got, "end\n") {
		t.Errorf("got %q, %v", got, err)
	}
	for name, res := range map[string]automation.Result{
		"run error":  {Error: "kein Prompt"},
		"no steps":   {},
		"step error": {Steps: []automation.Step{{Error: "% Invalid input"}}},
		"empty":      {Steps: []automation.Step{{Output: "  \n"}}},
	} {
		if _, err := runningConfigText(res); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestInsideDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Configs")
	for path, want := range map[string]bool{
		filepath.Join(dir, "sw1.cfg"):             true,
		filepath.Join(dir, "sub", "sw1.cfg"):      true,
		filepath.Join(dir, "..", "evil.exe"):      false,
		filepath.Join(filepath.Dir(dir), "x.cfg"): false,
		dir:                            false,
		`C:\Windows\System32\calc.exe`: false,
	} {
		if got := insideDir(dir, path); got != want {
			t.Errorf("insideDir(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSnippetReport(t *testing.T) {
	at := time.Date(2026, 11, 11, 18, 0, 0, 0, time.Local)
	res := automation.Result{Prompt: "sw1#", Steps: []automation.Step{
		{Command: "show clock", Output: "18:00:00.000 MEZ\n"},
		{Command: "show foo", Error: "% Invalid input"},
	}, Error: "Verbindung getrennt"}

	got := snippetReport("sw1", "Check", at, res)

	for _, want := range []string{"easyTTY – Check auf sw1, 11.11.2026 18:00:00", "! sw1#show clock\n18:00:00.000 MEZ\n", "! sw1#show foo\n! FEHLER: % Invalid input", "! Abgebrochen: Verbindung getrennt"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestOutputFileNameWithLabel(t *testing.T) {
	at := time.Date(2026, 11, 11, 18, 0, 5, 0, time.Local)

	if got := outputFileName("sw1", "Show Version", at, ".txt"); got != "sw1_Show_Version_2026-11-11_180005.txt" {
		t.Errorf("got %q", got)
	}
}

func TestWriteOutputsSplitsPerCommand(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 17, 10, 0, 0, 0, time.Local)
	res := automation.Result{Prompt: "sw1#", Error: "Zeitüberschreitung", Steps: []automation.Step{
		{Command: "show clock", Output: "10:00"},
		{Command: "show ip int br", Output: "Vlan1 up"},
	}}

	one, err := writeOutputs(dir, "sw1", "Ausgabe", at, res, false)
	if err != nil || len(one) != 1 {
		t.Fatalf("single file: %v %v", one, err)
	}
	split, err := writeOutputs(dir, "sw1", "Ausgabe", at, res, true)
	if err != nil || len(split) != 2 {
		t.Fatalf("split: %v %v", split, err)
	}

	if got := filepath.Base(split[1]); got != "sw1_show_ip_int_br_2026-11-17_100000.txt" {
		t.Errorf("name %s", got)
	}
	first, _ := os.ReadFile(split[0])
	last, _ := os.ReadFile(split[1])
	if !strings.Contains(string(first), "10:00") || strings.Contains(string(first), "Vlan1") || strings.Contains(string(first), "Abgebrochen") {
		t.Errorf("first file:\n%s", first)
	}
	if !strings.Contains(string(last), "Vlan1 up") || !strings.Contains(string(last), "Abgebrochen: Zeitüberschreitung") {
		t.Errorf("last file:\n%s", last)
	}
}

func TestWriteOutputsKeepsOneFileForOneCommand(t *testing.T) {
	res := automation.Result{Steps: []automation.Step{{Command: "show version"}}}

	paths, err := writeOutputs(t.TempDir(), "sw1", "Version", time.Now(), res, true)

	if err != nil || len(paths) != 1 || !strings.Contains(filepath.Base(paths[0]), "_Version_") {
		t.Errorf("%v %v", paths, err)
	}
}
