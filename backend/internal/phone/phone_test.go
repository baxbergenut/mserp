package phone

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {"  ", ""}, {"0123456789", "0123456789"},
		{"+1 (470) 334-4443", "4703344443"}, {"14703344443", "4703344443"},
		{" 470.334.4443 ", "4703344443"}, {"(470) 334-4443", "4703344443"},
	} {
		got, err := Normalize(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{"123", "123456789", "24703344443", "147033444430", "call 4703344443", "4703344443 x12", "4703344443/4703344444", "１２３４５６７８９０", "+", "470\n3344443"} {
		if _, err := Normalize(input); err == nil {
			t.Errorf("accepted invalid phone %q", input)
		}
		if Imported(input) != nil {
			t.Errorf("accepted invalid imported phone %q", input)
		}
	}
	if Imported("") != nil || *Imported("+1 (470) 334-4443") != "4703344443" {
		t.Fatal("imported phone normalization failed")
	}
	if Search("+1 (470) 334-4443") != "4703344443" || Search(" Driver ") != "Driver" {
		t.Fatal("contact search normalization failed")
	}
}
