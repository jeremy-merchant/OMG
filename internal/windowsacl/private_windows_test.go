//go:build windows

package windowsacl

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsPrivateUsesACESemanticsNotSDDLFormatting(t *testing.T) {
	owner, err := windows.StringToSid("S-1-5-21-1-2-3-1001")
	if err != nil {
		t.Fatal(err)
	}
	other, err := windows.StringToSid("S-1-5-21-1-2-3-1002")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		sddl string
		want bool
	}{
		{"protected owner full access", "O:" + owner.String() + "D:P(A;;FA;;;" + owner.String() + ")", true},
		{"unserialized protected flag", "O:" + owner.String() + "D:(A;;FA;;;" + owner.String() + ")", true},
		{"broad world entry", "O:" + owner.String() + "D:(A;;FA;;;" + owner.String() + ")(A;;FR;;;WD)", false},
		{"wrong owner", "O:" + other.String() + "D:(A;;FA;;;" + owner.String() + ")", false},
		{"wrong trustee", "O:" + owner.String() + "D:(A;;FA;;;" + other.String() + ")", false},
		{"insufficient access", "O:" + owner.String() + "D:(A;;FR;;;" + owner.String() + ")", false},
		{"inherited owner entry", "O:" + owner.String() + "D:(A;ID;FA;;;" + owner.String() + ")", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			descriptor, err := windows.SecurityDescriptorFromString(test.sddl)
			if err != nil {
				t.Fatal(err)
			}
			if got := IsPrivate(descriptor, owner); got != test.want {
				t.Fatalf("IsPrivate(%q) = %v; want %v", test.sddl, got, test.want)
			}
		})
	}
}
