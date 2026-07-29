package main

import "testing"

func TestLoadDialogIcon(t *testing.T) {
	icon, err := loadDialogIcon()
	if err != nil {
		t.Fatal(err)
	}
	if err := icon.Validate(); err != nil {
		t.Fatalf("invalid dialog icon: %v", err)
	}
	if icon.Width != dialogIconSize || icon.Height != dialogIconSize {
		t.Errorf("icon is %dx%d, want %dx%d",
			icon.Width, icon.Height, dialogIconSize, dialogIconSize)
	}

	visible := false
	for i := 3; i < len(icon.Pixels); i += 4 {
		if icon.Pixels[i] != 0 {
			visible = true
			break
		}
	}
	if !visible {
		t.Error("dialog icon is fully transparent")
	}
}
