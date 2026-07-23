package cli

import "testing"

func TestDecodeReceiptCommands(t *testing.T) {
	for _, args := range [][]string{
		{"receipt", "get", "--payload", `{"id":"receipt-1"}`},
		{"receipt", "list", "--payload", `{}`},
	} {
		request, err := Decode(args)
		if err.Code != "" {
			t.Fatalf("Decode(%v) = %v", args, err)
		}
		if request.Name != "receipt" || request.Subcommand == "" || !applicationCommandName(request.Name) {
			t.Fatalf("receipt request = %#v", request)
		}
	}
}
