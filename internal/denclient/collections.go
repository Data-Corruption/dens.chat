package denclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// PageCollectionItem is a collection item with its message opened for the page.
type PageCollectionItem struct {
	Message PageMessage `json:"message"`
	AddedBy string      `json:"added_by"`
	AddedAt int64       `json:"added_at"`
}

// PageCollectionItems is a collection and its opened items for the page.
type PageCollectionItems struct {
	Collection denproto.Collection  `json:"collection"`
	Items      []PageCollectionItem `json:"items"`
}

// Collections fetches all personal collections of this member and all shared
// collections in channels they currently see, with fresh visible counts.
func (m *Manager) Collections(ctx context.Context, denID string) ([]denproto.Collection, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	var list denproto.CollectionList
	if err := c.call(ctx, http.MethodGet, "/api/collections", nil, &list); err != nil {
		return nil, err
	}
	c.mu.Lock()
	var channels map[string]denproto.Channel
	if c.den != nil {
		channels = c.den.channels
	}
	_, ok := cleanCollections(list.Collections, channels)
	c.mu.Unlock()
	if !ok {
		return nil, errors.New("the den's answer is malformed")
	}
	if list.Collections == nil {
		list.Collections = []denproto.Collection{}
	}
	return list.Collections, nil
}

// CreatePersonalCollection creates a personal collection for this member on a den.
func (m *Manager) CreatePersonalCollection(ctx context.Context, denID, name string) (denproto.Collection, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Collection{}, err
	}
	clean, err := denproto.CleanCollectionName(name)
	if err != nil {
		return denproto.Collection{}, inputError(err)
	}
	var col denproto.Collection
	if err := c.call(ctx, http.MethodPost, "/api/collections", denproto.CollectionRequest{Name: &clean}, &col); err != nil {
		return denproto.Collection{}, err
	}
	if denproto.CheckCollection(col) != nil || col.ChannelID != "" {
		return denproto.Collection{}, errors.New("the den's answer is malformed")
	}
	return col, nil
}

// CreateChannelCollection creates a shared collection in a text channel, as staff.
func (m *Manager) CreateChannelCollection(ctx context.Context, denID, channelID, name, whoAdds string) (denproto.Collection, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Collection{}, err
	}
	if err := checkID("channel", channelID); err != nil {
		return denproto.Collection{}, err
	}
	clean, err := denproto.CleanCollectionName(name)
	if err != nil {
		return denproto.Collection{}, inputError(err)
	}
	req := denproto.CollectionRequest{Name: &clean}
	if whoAdds != "" {
		if err := denproto.CheckWhoAdds(whoAdds); err != nil {
			return denproto.Collection{}, inputError(err)
		}
		req.WhoAdds = &whoAdds
	}
	var col denproto.Collection
	if err := c.call(ctx, http.MethodPost, "/api/channels/"+channelID+"/collections", req, &col); err != nil {
		return denproto.Collection{}, err
	}
	if denproto.CheckCollection(col) != nil || col.ChannelID != channelID {
		return denproto.Collection{}, errors.New("the den's answer is malformed")
	}
	return col, nil
}

// UpdateCollection updates a collection's name and/or who_adds.
func (m *Manager) UpdateCollection(ctx context.Context, denID, collectionID string, req denproto.CollectionRequest) (denproto.Collection, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.Collection{}, err
	}
	if err := checkID("collection", collectionID); err != nil {
		return denproto.Collection{}, err
	}
	if req.Name != nil {
		clean, err := denproto.CleanCollectionName(*req.Name)
		if err != nil {
			return denproto.Collection{}, inputError(err)
		}
		req.Name = &clean
	}
	if req.WhoAdds != nil {
		if err := denproto.CheckWhoAdds(*req.WhoAdds); err != nil {
			return denproto.Collection{}, inputError(err)
		}
	}
	var col denproto.Collection
	if err := c.call(ctx, http.MethodPatch, "/api/collections/"+collectionID, req, &col); err != nil {
		return denproto.Collection{}, err
	}
	if denproto.CheckCollection(col) != nil || col.ID != collectionID {
		return denproto.Collection{}, errors.New("the den's answer is malformed")
	}
	return col, nil
}

// DeleteCollection deletes a collection.
func (m *Manager) DeleteCollection(ctx context.Context, denID, collectionID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	if err := checkID("collection", collectionID); err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/api/collections/"+collectionID, nil, nil)
}

// CollectionItems fetches a collection and its currently visible items across
// all pages, opening any DM or Notes messages (and keeping DM file keys so
// OpenFile works without loading DM history first).
func (m *Manager) CollectionItems(ctx context.Context, denID, collectionID string) (PageCollectionItems, error) {
	c, err := m.find(denID)
	if err != nil {
		return PageCollectionItems{}, err
	}
	if err := checkID("collection", collectionID); err != nil {
		return PageCollectionItems{}, err
	}
	var col denproto.Collection
	var raw []denproto.CollectionItem
	seen := map[string]bool{}
	var beforeAt, beforeID int64
	hasBefore := false
	totalReceived := 0
	for page := 0; ; page++ {
		if page >= denproto.MaxCollectionItems {
			return PageCollectionItems{}, errMalformed
		}
		path := "/api/collections/" + collectionID + "/items"
		if hasBefore {
			q := url.Values{}
			q.Set("before_at", strconv.FormatInt(beforeAt, 10))
			q.Set("before_id", denproto.FormatID(beforeID))
			path += "?" + q.Encode()
		}
		var resp denproto.CollectionItems
		if err := c.call(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return PageCollectionItems{}, err
		}
		if denproto.CheckCollection(resp.Collection) != nil || resp.Collection.ID != collectionID {
			return PageCollectionItems{}, errMalformed
		}
		if page == 0 {
			col = resp.Collection
		} else if resp.Collection.ChannelID != col.ChannelID {
			return PageCollectionItems{}, errMalformed
		}
		totalReceived += len(resp.Items)
		if totalReceived > denproto.MaxCollectionItems {
			return PageCollectionItems{}, errMalformed
		}
		for _, item := range resp.Items {
			if denproto.CheckCollectionItem(item) != nil {
				return PageCollectionItems{}, errMalformed
			}
			if col.ChannelID != "" && item.Message.ChannelID != col.ChannelID {
				return PageCollectionItems{}, errMalformed
			}
			if seen[item.Message.ID] {
				continue
			}
			seen[item.Message.ID] = true
			raw = append(raw, item)
		}
		if !resp.HasMore {
			break
		}
		if len(resp.Items) == 0 || resp.NextBeforeAt < 0 || totalReceived >= denproto.MaxCollectionItems {
			return PageCollectionItems{}, errMalformed
		}
		nextID, err := denproto.ParseID(resp.NextBeforeID)
		if err != nil {
			return PageCollectionItems{}, errMalformed
		}
		if hasBefore && (resp.NextBeforeAt > beforeAt || (resp.NextBeforeAt == beforeAt && nextID >= beforeID)) {
			return PageCollectionItems{}, errMalformed
		}
		beforeAt, beforeID, hasBefore = resp.NextBeforeAt, nextID, true
	}
	var knownChannels map[string]bool
	if col.ChannelID == "" {
		c.mu.Lock()
		if c.den != nil {
			knownChannels = make(map[string]bool, len(c.den.channels))
			for id := range c.den.channels {
				knownChannels[id] = true
			}
		}
		c.mu.Unlock()
	}
	live := make([]denproto.CollectionItem, 0, len(raw))
	byChannel := map[string][]denproto.Message{}
	var channelOrder []string
	for _, item := range raw {
		if col.ChannelID == "" && !knownChannels[item.Message.ChannelID] {
			continue
		}
		if m.pastTime(item.Message) {
			continue
		}
		live = append(live, item)
		chID := item.Message.ChannelID
		if _, seen := byChannel[chID]; !seen {
			channelOrder = append(channelOrder, chID)
		}
		byChannel[chID] = append(byChannel[chID], item.Message)
	}
	for _, chID := range channelOrder {
		c.fetchKeys(ctx, chID, byChannel[chID])
	}
	items := make([]PageCollectionItem, 0, len(live))
	for _, item := range live {
		pm := c.open(item.Message)
		pm.Nonce = nil
		items = append(items, PageCollectionItem{
			Message: pm,
			AddedBy: item.AddedBy,
			AddedAt: item.AddedAt,
		})
	}
	col.Count = len(items)
	return PageCollectionItems{Collection: col, Items: items}, nil
}

// AddCollectionItem adds a message to a collection.
func (m *Manager) AddCollectionItem(ctx context.Context, denID, collectionID, messageID string) (denproto.CollectionItemEvent, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	if err := checkID("collection", collectionID); err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	if err := checkID("message", messageID); err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	var ev denproto.CollectionItemEvent
	if err := c.call(ctx, http.MethodPost, "/api/collections/"+collectionID+"/items", denproto.CollectionItemRequest{MessageID: messageID}, &ev); err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	if denproto.CheckCollectionItemEvent(ev) != nil || ev.CollectionID != collectionID || ev.MessageID != messageID {
		return denproto.CollectionItemEvent{}, errors.New("the den's answer is malformed")
	}
	return ev, nil
}

// RemoveCollectionItem removes a message from a collection.
func (m *Manager) RemoveCollectionItem(ctx context.Context, denID, collectionID, messageID string) (denproto.CollectionItemEvent, error) {
	c, err := m.find(denID)
	if err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	if err := checkID("collection", collectionID); err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	if err := checkID("message", messageID); err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	var ev denproto.CollectionItemEvent
	if err := c.call(ctx, http.MethodDelete, "/api/collections/"+collectionID+"/items/"+messageID, nil, &ev); err != nil {
		return denproto.CollectionItemEvent{}, err
	}
	if denproto.CheckCollectionItemEvent(ev) != nil || ev.CollectionID != collectionID || ev.MessageID != messageID {
		return denproto.CollectionItemEvent{}, errors.New("the den's answer is malformed")
	}
	return ev, nil
}

// MessageCollections returns the IDs of accessible collections currently holding messageID.
func (m *Manager) MessageCollections(ctx context.Context, denID, messageID string) ([]string, error) {
	c, err := m.find(denID)
	if err != nil {
		return nil, err
	}
	if err := checkID("message", messageID); err != nil {
		return nil, err
	}
	var resp denproto.MessageCollections
	if err := c.call(ctx, http.MethodGet, "/api/messages/"+messageID+"/collections", nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.CollectionIDs) > denproto.MaxPersonalCollections+denproto.MaxChannelCollections {
		return nil, errMalformed
	}
	for i, id := range resp.CollectionIDs {
		if !validID(id) || slices.Contains(resp.CollectionIDs[:i], id) {
			return nil, errMalformed
		}
	}
	if resp.CollectionIDs == nil {
		resp.CollectionIDs = []string{}
	}
	return resp.CollectionIDs, nil
}
