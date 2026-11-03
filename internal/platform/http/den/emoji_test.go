package den

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// emojiPNG is a still emoji, as the page draws one.
func emojiPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestEmojiOverHTTP checks the den's emoji's routes: staff add, rename and
// delete emoji, a member can't, a taken name is refused as taken, and every
// member opens an emoji's image.
func TestEmojiOverHTTP(t *testing.T) {
	f := newFixture(t)
	ownerToken, _ := f.owner()
	bobToken := f.member(ownerToken, "bob")
	send := func(method, path string, body any, token denproto.Bytes) *http.Response {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, f.srv.URL+path, bytes.NewReader(data))
		req.Header.Set(denproto.HeaderVersion, "1")
		req.Header.Set("Authorization", "Bearer "+token.String())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	upload := func(token denproto.Bytes) denproto.File {
		file := emojiPNG(t, 64, 64)
		resp := f.upload(token, "emoji.png", bytes.NewReader(file), int64(len(file)))
		defer resp.Body.Close()
		var up denproto.File
		if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&up) != nil {
			t.Fatalf("upload an emoji: %d %v", resp.StatusCode, denproto.ReadError(resp))
		}
		return up
	}
	add := func(token denproto.Bytes, name string) *http.Response {
		return f.post("/api/emoji", denproto.EmojiRequest{Name: name, FileID: upload(token).ID}, bearer(token))
	}

	wantError(t, add(bobToken, "mine"), http.StatusForbidden, denproto.CodeForbidden)
	resp := add(ownerToken, "Wave")
	var wave denproto.Emoji
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&wave) != nil || wave.Name != "wave" || denproto.CheckEmoji(wave) != nil {
		t.Fatalf("add: %d %+v", resp.StatusCode, wave)
	}
	resp.Body.Close()
	wantError(t, add(ownerToken, "WAVE"), http.StatusConflict, denproto.CodeEmojiNameTaken)
	wantError(t, add(ownerToken, "no spaces"), http.StatusBadRequest, denproto.CodeInvalidField)

	// Every member opens its image.
	if resp := send(http.MethodGet, "/api/files/"+wave.Image.ID, nil, bobToken); resp.StatusCode != http.StatusOK {
		t.Errorf("a member opening an emoji's image: %d", resp.StatusCode)
	}

	wantError(t, send(http.MethodPatch, "/api/emoji/"+wave.ID, denproto.EmojiUpdate{Name: "hi"}, bobToken), http.StatusForbidden, denproto.CodeForbidden)
	resp = send(http.MethodPatch, "/api/emoji/"+wave.ID, denproto.EmojiUpdate{Name: "Hello"}, ownerToken)
	var renamed denproto.Emoji
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&renamed) != nil || renamed.Name != "hello" {
		t.Fatalf("rename: %d %+v", resp.StatusCode, renamed)
	}
	resp.Body.Close()
	resp = add(ownerToken, "bye")
	var bye denproto.Emoji
	json.NewDecoder(resp.Body).Decode(&bye)
	resp.Body.Close()
	wantError(t, send(http.MethodPatch, "/api/emoji/"+bye.ID, denproto.EmojiUpdate{Name: "HELLO"}, ownerToken), http.StatusConflict, denproto.CodeEmojiNameTaken)
	wantError(t, send(http.MethodDelete, "/api/emoji/"+wave.ID, nil, bobToken), http.StatusForbidden, denproto.CodeForbidden)
	if resp := send(http.MethodDelete, "/api/emoji/"+wave.ID, nil, ownerToken); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	wantError(t, send(http.MethodDelete, "/api/emoji/"+wave.ID, nil, ownerToken), http.StatusNotFound, denproto.CodeNotFound)
	if resp := send(http.MethodGet, "/api/files/"+wave.Image.ID, nil, ownerToken); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a deleted emoji's image: %d", resp.StatusCode)
	}
}
