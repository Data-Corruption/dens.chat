package den

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestRolesOverHTTP checks the custom role HTTP routes: staff create,
// rename, assign and delete roles; members are forbidden; duplicate names
// return CodeRoleNameTaken; and non-staff never learn hidden roles via
// GET /api/members/{id}.
func TestRolesOverHTTP(t *testing.T) {
	f := newFixture(t)
	ownerToken, _ := f.owner()
	bobToken := f.member(ownerToken, "bob")
	carolToken := f.member(ownerToken, "carol")

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

	// Member forbidden on POST /api/roles.
	wantError(t, send(http.MethodPost, "/api/roles", denproto.DenRoleRequest{Name: "Raiders"}, bobToken), http.StatusForbidden, denproto.CodeForbidden)

	// Owner creates Raiders; duplicate name (any case) returns 409 role_name_taken.
	resp := send(http.MethodPost, "/api/roles", denproto.DenRoleRequest{Name: "Raiders"}, ownerToken)
	var raiders denproto.Role
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&raiders) != nil || raiders.Name != "Raiders" {
		t.Fatalf("create role: %d %+v", resp.StatusCode, raiders)
	}
	resp.Body.Close()
	wantError(t, send(http.MethodPost, "/api/roles", denproto.DenRoleRequest{Name: "raiders"}, ownerToken), http.StatusConflict, denproto.CodeRoleNameTaken)

	// Rename role.
	wantError(t, send(http.MethodPatch, "/api/roles/"+raiders.ID, denproto.DenRoleRequest{Name: "X"}, bobToken), http.StatusForbidden, denproto.CodeForbidden)
	resp = send(http.MethodPatch, "/api/roles/"+raiders.ID, denproto.DenRoleRequest{Name: "Raid Team"}, ownerToken)
	var renamed denproto.Role
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&renamed) != nil || renamed.Name != "Raid Team" {
		t.Fatalf("rename role: %d %+v", resp.StatusCode, renamed)
	}
	resp.Body.Close()

	// Create a role-gated channel using raiders.ID.
	name := "raids"
	roleIDs := []string{raiders.ID}
	resp = send(http.MethodPost, "/api/channels", denproto.ChannelRequest{Name: &name, RoleIDs: &roleIDs}, ownerToken)
	var ch denproto.Channel
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&ch) != nil || !slices.Equal(ch.RoleIDs, roleIDs) {
		t.Fatalf("create role channel: %d %+v", resp.StatusCode, ch)
	}
	resp.Body.Close()

	// Find bob's member ID from a session check via /api/me (bob updates display name or look up ID "2").
	bobID := "2"
	wantError(t, send(http.MethodPut, "/api/members/"+bobID+"/roles", denproto.MemberRolesRequest{RoleIDs: roleIDs}, bobToken), http.StatusForbidden, denproto.CodeForbidden)
	resp = send(http.MethodPut, "/api/members/"+bobID+"/roles", denproto.MemberRolesRequest{RoleIDs: roleIDs}, ownerToken)
	var updated denproto.Member
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&updated) != nil || !slices.Equal(updated.RoleIDs, roleIDs) {
		t.Fatalf("set member roles: %d %+v", resp.StatusCode, updated)
	}
	resp.Body.Close()

	// Carol (no role) does not see bob's role on GET /api/members/2; bob does.
	resp = send(http.MethodGet, "/api/members/"+bobID, nil, carolToken)
	var carolProf denproto.MemberProfile
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&carolProf) != nil || len(carolProf.RoleIDs) != 0 {
		t.Fatalf("carol profile of bob: %d %+v", resp.StatusCode, carolProf)
	}
	resp.Body.Close()

	resp = send(http.MethodGet, "/api/members/"+bobID, nil, bobToken)
	var bobProf denproto.MemberProfile
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&bobProf) != nil || !slices.Equal(bobProf.RoleIDs, roleIDs) {
		t.Fatalf("bob profile of bob: %d %+v", resp.StatusCode, bobProf)
	}
	resp.Body.Close()

	// Delete role.
	wantError(t, send(http.MethodDelete, "/api/roles/"+raiders.ID, nil, bobToken), http.StatusForbidden, denproto.CodeForbidden)
	resp = send(http.MethodDelete, "/api/roles/"+raiders.ID, nil, ownerToken)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete role: %d", resp.StatusCode)
	}
	resp.Body.Close()
	wantError(t, send(http.MethodDelete, "/api/roles/"+raiders.ID, nil, ownerToken), http.StatusNotFound, denproto.CodeNotFound)
}
