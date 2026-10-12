package prompt

import (
	"bytes"
	"testing"
)

func TestSecretR(t *testing.T) {
	var output bytes.Buffer
	got, err := secretR(bytes.NewBufferString("  keep spaces  \n"), &output, "Password")
	if err != nil {
		t.Fatalf("secretR: %v", err)
	}
	if got != "  keep spaces  " {
		t.Fatalf("got %q, want spaces preserved", got)
	}
	if output.String() != "Password: " {
		t.Fatalf("prompt = %q, want %q", output.String(), "Password: ")
	}
}

func TestYesNoR(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    bool
		wantErr bool
	}{
		{"yes-lower", "y\n", true, false},
		{"yes-upper", "YES\n", true, false},
		{"no-mixed", "No\n", false, false},
		{"retry-after-junk", "maybe\nn\n", false, false},
		{"eof", "", false, true},
		{"eof-after-junk", "maybe\n", false, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := yesNoR(bytes.NewBufferString(tc.in), "continue?")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
