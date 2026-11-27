package client

import (
	"net/http"

	"github.com/Data-Corruption/dens.chat/internal/denproto"

	"github.com/go-chi/chi/v5"
)

func (rt *router) mountCollections(r chi.Router) {
	r.Get("/api/dens/{den}/collections", rt.handleCollections)
	r.Post("/api/dens/{den}/collections", rt.handleCreatePersonalCollection)
	r.Post("/api/dens/{den}/channels/{channel}/collections", rt.handleCreateChannelCollection)
	r.Patch("/api/dens/{den}/collections/{collection}", rt.handleUpdateCollection)
	r.Delete("/api/dens/{den}/collections/{collection}", rt.handleDeleteCollection)
	r.Get("/api/dens/{den}/collections/{collection}/items", rt.handleCollectionItems)
	r.Post("/api/dens/{den}/collections/{collection}/items", rt.handleAddCollectionItem)
	r.Delete("/api/dens/{den}/collections/{collection}/items/{message}", rt.handleRemoveCollectionItem)
	r.Get("/api/dens/{den}/messages/{message}/collections", rt.handleMessageCollections)
}

func (rt *router) handleCollections(w http.ResponseWriter, r *http.Request) {
	cols, err := rt.a.Dens.Collections(r.Context(), chi.URLParam(r, "den"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, denproto.CollectionList{Collections: cols})
}

func (rt *router) handleCreatePersonalCollection(w http.ResponseWriter, r *http.Request) {
	var req denproto.CollectionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	col, err := rt.a.Dens.CreatePersonalCollection(r.Context(), chi.URLParam(r, "den"), *req.Name)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, col)
}

func (rt *router) handleCreateChannelCollection(w http.ResponseWriter, r *http.Request) {
	var req denproto.CollectionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil {
		jsonError(w, http.StatusBadRequest, "name is required")
		return
	}
	whoAdds := ""
	if req.WhoAdds != nil {
		whoAdds = *req.WhoAdds
	}
	col, err := rt.a.Dens.CreateChannelCollection(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "channel"), *req.Name, whoAdds)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, col)
}

func (rt *router) handleUpdateCollection(w http.ResponseWriter, r *http.Request) {
	var req denproto.CollectionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	col, err := rt.a.Dens.UpdateCollection(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "collection"), req)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, col)
}

func (rt *router) handleDeleteCollection(w http.ResponseWriter, r *http.Request) {
	if err := rt.a.Dens.DeleteCollection(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "collection")); err != nil {
		rt.denError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *router) handleCollectionItems(w http.ResponseWriter, r *http.Request) {
	items, err := rt.a.Dens.CollectionItems(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "collection"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, items)
}

func (rt *router) handleAddCollectionItem(w http.ResponseWriter, r *http.Request) {
	var req denproto.CollectionItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ev, err := rt.a.Dens.AddCollectionItem(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "collection"), req.MessageID)
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, ev)
}

func (rt *router) handleRemoveCollectionItem(w http.ResponseWriter, r *http.Request) {
	ev, err := rt.a.Dens.RemoveCollectionItem(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "collection"), chi.URLParam(r, "message"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, ev)
}

func (rt *router) handleMessageCollections(w http.ResponseWriter, r *http.Request) {
	ids, err := rt.a.Dens.MessageCollections(r.Context(), chi.URLParam(r, "den"), chi.URLParam(r, "message"))
	if err != nil {
		rt.denError(w, r, err)
		return
	}
	writeJSON(w, denproto.MessageCollections{CollectionIDs: ids})
}
