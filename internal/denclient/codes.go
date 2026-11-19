package denclient

import (
	"encoding/hex"
	"errors"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"rsc.io/qr/coding"
)

// Sign-in codes (M22.6), made here and drawn by the page: Add device's,
// for a new device to scan the den's address and the member's username
// from, and the digits a device shows as it approves one.

// MaxCodeVersion is the largest QR code Dens makes, and the page reads:
// version 10, 57 modules a side, which holds a den's address of 168
// characters with the longest username.
const MaxCodeVersion = 10

// Code is a QR code for the page to draw: Size modules a side, each row
// in hex, its first module the top bit of its first byte, a dark one set.
// The page draws the quiet zone around it.
type Code struct {
	Size int      `json:"size"`
	Rows []string `json:"rows"`
}

// QRCode makes the code of text at the error correction level that
// survives a screen's glare and a camera's blur, M, 15% of it.
func QRCode(text string) (Code, error) {
	c, _, _, err := encode(text)
	if err != nil {
		return Code{}, err
	}
	width := (c.Size + 7) / 8
	rows := make([]string, c.Size)
	for y := range rows {
		rows[y] = hex.EncodeToString(c.Bitmap[y*c.Stride : y*c.Stride+width])
	}
	return Code{Size: c.Size, Rows: rows}, nil
}

// encode makes the smallest code from version 2 that holds text, in the
// densest mode it takes, with the mask that scores best. From version 2 a
// code has an alignment pattern, which a reader corrects a tilted camera
// by. rsc.io/qr leaves picking the mask to its caller, and a mask left as
// it comes can draw what a reader takes for a finder.
func encode(text string) (*coding.Code, coding.Version, coding.Mask, error) {
	var enc coding.Encoding = coding.String(text)
	if coding.Num(text).Check() == nil {
		enc = coding.Num(text)
	} else if coding.Alpha(text).Check() == nil {
		enc = coding.Alpha(text)
	}
	v := coding.Version(2)
	for enc.Bits(v) > v.DataBytes(coding.M)*8 {
		if v++; v > MaxCodeVersion {
			return nil, 0, 0, errors.New("too long for a code")
		}
	}
	var best *coding.Code
	var mask coding.Mask
	score := 0
	for m := coding.Mask(0); m < 8; m++ {
		p, err := coding.NewPlan(v, coding.M, m)
		if err != nil {
			return nil, 0, 0, err
		}
		c, err := p.Encode(enc)
		if err != nil {
			return nil, 0, 0, err
		}
		if s := penalty(c); best == nil || s < score {
			best, mask, score = c, m, s
		}
	}
	return best, v, mask, nil
}

// penalty scores a code as ISO/IEC 18004 does to pick its mask: runs of
// five or more modules alike, 2 by 2 blocks alike, stretches that look
// like a finder's middle beside four light modules, and dark modules far
// from half of them. Past the code's edge is its quiet zone, light.
func penalty(c *coding.Code) int {
	n := c.Size
	score := 0
	for i := 0; i < n; i++ {
		for _, across := range []bool{true, false} {
			dark := func(j int) bool {
				if across {
					return c.Black(j, i)
				}
				return c.Black(i, j)
			}
			run := 1
			for j := 1; j <= n; j++ {
				if j < n && dark(j) == dark(j-1) {
					run++
					continue
				}
				if run >= 5 {
					score += run - 2
				}
				run = 1
			}
			light := func(from, to int) bool {
				for k := from; k < to; k++ {
					if dark(k) {
						return false
					}
				}
				return true
			}
			for j := 0; j+6 < n; j++ {
				if dark(j) && !dark(j+1) && dark(j+2) && dark(j+3) && dark(j+4) && !dark(j+5) && dark(j+6) && (light(j-4, j) || light(j+7, j+11)) {
					score += 40
				}
			}
		}
	}
	darks := 0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			d := c.Black(x, y)
			if d {
				darks++
			}
			if x+1 < n && y+1 < n && c.Black(x+1, y) == d && c.Black(x, y+1) == d && c.Black(x+1, y+1) == d {
				score += 3
			}
		}
	}
	off := darks*100/(n*n) - 50
	if off < 0 {
		off = -off
	}
	return score + off/5*10
}

// DeviceCodeView is Add device's code for a den, and what it holds, which
// its screen shows beside it.
type DeviceCodeView struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Code     Code   `json:"code"`
}

// DeviceCode is Add device's code for a den: its address and the member's
// username there.
func (m *Manager) DeviceCode(denID string) (DeviceCodeView, error) {
	c, err := m.find(denID)
	if err != nil {
		return DeviceCodeView{}, err
	}
	c.mu.Lock()
	url, username := c.profile.URL, c.profile.Member.Username
	c.mu.Unlock()
	text, err := denproto.DeviceCode(url, username)
	if err != nil {
		return DeviceCodeView{}, inputError(err)
	}
	code, err := QRCode(text)
	if err != nil {
		return DeviceCodeView{}, inputError(errors.New("that den's address doesn't fit in a code"))
	}
	return DeviceCodeView{URL: url, Username: username, Code: code}, nil
}

// DigitsCode is the code of the digits this device shows for a sign-in
// it answered, while it shows them.
func (m *Manager) DigitsCode(denID, requestID string) (Code, error) {
	c, err := m.find(denID)
	if err != nil {
		return Code{}, err
	}
	c.mu.Lock()
	half := ""
	if a := c.approvals[requestID]; a != nil && a.result != nil {
		half = denproto.CheckHalf(a.result.Check, false)
	} else if v, ok := c.approved[requestID]; ok {
		half = digitsOnly(v.Half)
	}
	c.mu.Unlock()
	if half == "" {
		return Code{}, inputError(errors.New("this device shows no digits for that sign-in"))
	}
	text, err := denproto.DigitsCode(half)
	if err != nil {
		return Code{}, err
	}
	return QRCode(text)
}

// digitsOnly takes the spaces out of digits shown in groups.
func digitsOnly(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
