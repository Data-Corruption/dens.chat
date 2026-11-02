package client

import (
	"errors"
	"image"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
)

// The part of an animated picture a member framed comes as four whole
// numbers, and anything else is the page's mistake, said as such (M15.1).
func TestParseCrop(t *testing.T) {
	if r, err := parseCrop("10,20,300,100"); err != nil || r != image.Rect(10, 20, 310, 120) {
		t.Fatalf("%v %v", r, err)
	}
	var input *denclient.InputError
	for _, s := range []string{"", "1,2,3", "1,2,3,4,5", "a,2,3,4", "-1,2,3,4", "1,2,3,70000", "1.5,2,3,4", "1, 2,3,4"} {
		if _, err := parseCrop(s); !errors.As(err, &input) {
			t.Errorf("%q: %v", s, err)
		}
	}
}
