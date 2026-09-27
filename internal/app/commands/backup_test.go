package commands

import (
	"os"
	"testing"
)

func TestReadPasswordFromStdin(t *testing.T) {
	for name, input := range map[string]string{
		"lf":         "correct horse\n",
		"crlf":       "correct horse\r\n",
		"no newline": "correct horse",
		// What Windows PowerShell 5.1 pipes when $OutputEncoding is UTF-8.
		"byte order mark": "\xef\xbb\xbfcorrect horse\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.WriteString(input); err != nil {
				t.Fatal(err)
			}
			w.Close()
			stdin := os.Stdin
			os.Stdin = r
			t.Cleanup(func() {
				os.Stdin = stdin
				r.Close()
			})
			got, err := readPassword(true, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != "correct horse" {
				t.Fatalf("readPassword = %q, want %q", got, "correct horse")
			}
		})
	}
}
