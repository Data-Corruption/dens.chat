package den

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

func TestCollectionsOverHTTP(t *testing.T) {
	f := newFixture(t)
	ownerToken, _ := f.owner()
	bobToken := f.member(ownerToken, "bob")

	send := func(method, path string, body any, token denproto.Bytes) *http.Response {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, f.srv.URL+path, bytes.NewReader(data))
		req.Header.Set(denproto.HeaderVersion, "3")
		req.Header.Set("Authorization", "Bearer "+token.String())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Create a text channel and post a message.
	chName := "general"
	resp := send(http.MethodPost, "/api/channels", denproto.ChannelRequest{Name: &chName}, ownerToken)
	var ch denproto.Channel
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&ch) != nil {
		t.Fatalf("create channel: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = send(http.MethodPost, "/api/channels/"+ch.ID+"/messages", denproto.SendRequest{
		Nonce: denproto.Random(denproto.NonceBytes),
		Text:  "important guide",
	}, bobToken)
	var msg denproto.Message
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&msg) != nil {
		t.Fatalf("send msg: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Member forbidden on shared channel collection create.
	colName := "Guides"
	wantError(t, send(http.MethodPost, "/api/channels/"+ch.ID+"/collections", denproto.CollectionRequest{Name: &colName}, bobToken),
		http.StatusForbidden, denproto.CodeForbidden)

	// Owner creates shared collection; duplicate returns 409 collection_name_taken.
	who := denproto.CollectionAddEveryone
	resp = send(http.MethodPost, "/api/channels/"+ch.ID+"/collections", denproto.CollectionRequest{Name: &colName, WhoAdds: &who}, ownerToken)
	var shared denproto.Collection
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&shared) != nil || shared.Name != "Guides" {
		t.Fatalf("create shared: %d %+v", resp.StatusCode, shared)
	}
	resp.Body.Close()
	dup := "guides"
	wantError(t, send(http.MethodPost, "/api/channels/"+ch.ID+"/collections", denproto.CollectionRequest{Name: &dup}, ownerToken),
		http.StatusConflict, denproto.CodeCollectionNameTaken)

	// Bob creates personal collection; owner gets 404 trying to view it.
	pName := "Saved"
	resp = send(http.MethodPost, "/api/collections", denproto.CollectionRequest{Name: &pName}, bobToken)
	var personal denproto.Collection
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&personal) != nil || personal.ChannelID != "" {
		t.Fatalf("create personal: %d %+v", resp.StatusCode, personal)
	}
	resp.Body.Close()
	wantError(t, send(http.MethodGet, "/api/collections/"+personal.ID+"/items", nil, ownerToken),
		http.StatusNotFound, denproto.CodeNotFound)

	// Bob adds message to both shared and personal collections.
	for _, cid := range []string{shared.ID, personal.ID} {
		resp = send(http.MethodPost, "/api/collections/"+cid+"/items", denproto.CollectionItemRequest{MessageID: msg.ID}, bobToken)
		var ev denproto.CollectionItemEvent
		if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&ev) != nil || ev.Count != 1 {
			t.Fatalf("add item to %s: %d %+v", cid, resp.StatusCode, ev)
		}
		resp.Body.Close()
	}

	// GET /api/messages/{id}/collections returns both for bob, only shared for owner.
	resp = send(http.MethodGet, "/api/messages/"+msg.ID+"/collections", nil, bobToken)
	var mc denproto.MessageCollections
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&mc) != nil || !slices.Equal(mc.CollectionIDs, []string{shared.ID, personal.ID}) {
		t.Fatalf("bob msg collections: %d %+v", resp.StatusCode, mc)
	}
	resp.Body.Close()

	resp = send(http.MethodGet, "/api/messages/"+msg.ID+"/collections", nil, ownerToken)
	var ownerMC denproto.MessageCollections
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&ownerMC) != nil || !slices.Equal(ownerMC.CollectionIDs, []string{shared.ID}) {
		t.Fatalf("owner msg collections: %d %+v", resp.StatusCode, ownerMC)
	}
	resp.Body.Close()

	// GET /api/collections and GET /api/collections/{id}/items.
	resp = send(http.MethodGet, "/api/collections", nil, bobToken)
	var list denproto.CollectionList
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&list) != nil || len(list.Collections) != 2 {
		t.Fatalf("list collections: %d %+v", resp.StatusCode, list)
	}
	resp.Body.Close()

	resp = send(http.MethodGet, "/api/collections/"+shared.ID+"/items", nil, bobToken)
	var items denproto.CollectionItems
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&items) != nil || len(items.Items) != 1 || items.Items[0].Message.Text != "important guide" {
		t.Fatalf("collection items: %d %+v", resp.StatusCode, items)
	}
	resp.Body.Close()

	// Cursor query parameter validation and keyset filtering on GET /api/collections/{id}/items.
	wantError(t, send(http.MethodGet, "/api/collections/"+shared.ID+"/items?before_at=100", nil, bobToken),
		http.StatusBadRequest, denproto.CodeInvalidField)
	wantError(t, send(http.MethodGet, "/api/collections/"+shared.ID+"/items?before_id=1", nil, bobToken),
		http.StatusBadRequest, denproto.CodeInvalidField)
	wantError(t, send(http.MethodGet, "/api/collections/"+shared.ID+"/items?before_at=-1&before_id=1", nil, bobToken),
		http.StatusBadRequest, denproto.CodeInvalidField)
	wantError(t, send(http.MethodGet, "/api/collections/"+shared.ID+"/items?before_at=100&before_id=abc", nil, bobToken),
		http.StatusBadRequest, denproto.CodeInvalidField)
	cursorURL := fmt.Sprintf("/api/collections/%s/items?before_at=%d&before_id=%s", shared.ID, items.Items[0].AddedAt, items.Items[0].Message.ID)
	resp = send(http.MethodGet, cursorURL, nil, bobToken)
	var afterCursor denproto.CollectionItems
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&afterCursor) != nil || len(afterCursor.Items) != 0 || afterCursor.HasMore || afterCursor.Collection.Count != 1 {
		t.Fatalf("after cursor: %d %+v", resp.StatusCode, afterCursor)
	}
	resp.Body.Close()

	// Rename personal, remove item, delete both.
	renamed := "Later"
	resp = send(http.MethodPatch, "/api/collections/"+personal.ID, denproto.CollectionRequest{Name: &renamed}, bobToken)
	var upd denproto.Collection
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&upd) != nil || upd.Name != "Later" {
		t.Fatalf("update personal: %d %+v", resp.StatusCode, upd)
	}
	resp.Body.Close()

	resp = send(http.MethodDelete, "/api/collections/"+shared.ID+"/items/"+msg.ID, nil, bobToken)
	var rem denproto.CollectionItemEvent
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&rem) != nil || rem.Count != 0 {
		t.Fatalf("remove item: %d %+v", resp.StatusCode, rem)
	}
	resp.Body.Close()

	resp = send(http.MethodDelete, "/api/collections/"+personal.ID, nil, bobToken)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete personal: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = send(http.MethodDelete, "/api/collections/"+shared.ID, nil, ownerToken)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete shared: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
