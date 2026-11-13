package store

import "testing"

func TestFormatName(t *testing.T) {
	cases := []struct {
		nameCase, spaces, in, want string
	}{
		{CaseKeep, SpacesKeep, "  Core  Switch 1 ", "Core Switch 1"},
		{CaseLower, SpacesKeep, "Core Switch 1", "core switch 1"},
		{CaseUpper, SpacesUnderscore, "core switch 1", "CORE_SWITCH_1"},
		{CaseCapitalize, SpacesKeep, "cORE sWITCH", "Core switch"},
		{CaseCapitalize, SpacesUnderscore, "rz nord übergabe", "Rz_nord_übergabe"},
		{CaseTitle, SpacesDash, "rz nord  ÜBERGABE", "Rz-Nord-Übergabe"},
		{CaseLower, SpacesDash, "Switch Konsole (COM6)", "switch-konsole-(com6)"},
		{CaseTitle, SpacesKeep, "äpfel", "Äpfel"},
		{CaseTitle, SpacesUnderscore, "test switch", "Test_Switch"},
		{CaseTitle, SpacesUnderscore, "test_switch", "Test_Switch"},
		{CaseTitle, SpacesKeep, "core-sw1 nord", "Core-Sw1 Nord"},
		{CaseLower, SpacesUnderscore, "   ", ""},
	}
	for _, c := range cases {
		s := Settings{NameCase: c.nameCase, NameSpaces: c.spaces}
		if got := s.FormatName(c.in); got != c.want {
			t.Errorf("%q/%q %q: got %q want %q", c.nameCase, c.spaces, c.in, got, c.want)
		}
	}
}

func TestNamingSanitize(t *testing.T) {
	bad := Settings{NameCase: "shout", NameSpaces: "_"}.Sanitize()
	if bad.NameCase != CaseKeep || bad.NameSpaces != SpacesKeep {
		t.Fatalf("invalid naming must reset: %+v", bad)
	}
	ok := Settings{NameCase: CaseCapitalize, NameSpaces: SpacesUnderscore}.Sanitize()
	if ok.NameCase != CaseCapitalize || ok.NameSpaces != SpacesUnderscore {
		t.Fatal("valid naming must be kept")
	}
	if ValidNaming("lower", "+") {
		t.Fatal("unknown separator must be invalid")
	}
}
