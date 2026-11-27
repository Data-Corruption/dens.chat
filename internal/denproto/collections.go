package denproto

import (
	"errors"
	"fmt"
)

// Collections: named sets of saved messages. Shared collections belong to one
// text channel, inherit its visibility and are managed by staff; personal
// collections belong to one member across the whole den, are private to that
// member and filter items through what they can currently see.
const (
	MaxChannelCollections  = 20
	MaxPersonalCollections = 20
	MaxCollectionItems     = 200
	MaxCollectionNameRunes = 50
	maxCollectionNameBytes = 1 << 10

	CollectionAddEveryone = "everyone"
	CollectionAddStaff    = "staff"

	// CodeCollectionNameTaken reports a collection name already used in the
	// same channel or by the same member (compared case-insensitively).
	CodeCollectionNameTaken = "collection_name_taken"

	// Event types for collections.
	EventCollectionCreated     = "collection.created"
	EventCollectionUpdated     = "collection.updated"
	EventCollectionDeleted     = "collection.deleted"
	EventCollectionItemAdded   = "collection.item_added"
	EventCollectionItemRemoved = "collection.item_removed"
)

// Collection is a shared channel collection (ChannelID and WhoAdds set) or a
// personal collection (ChannelID and WhoAdds empty).
type Collection struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id,omitempty"`
	Name      string `json:"name"`
	WhoAdds   string `json:"who_adds,omitempty"`
	Count     int    `json:"count"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// CollectionRequest creates or updates a collection. On an update, nil fields
// stay as they are.
type CollectionRequest struct {
	Name    *string `json:"name,omitempty"`
	WhoAdds *string `json:"who_adds,omitempty"`
}

// CollectionItem is one saved message in a collection, with who added it and
// when (unix milliseconds).
type CollectionItem struct {
	Message Message `json:"message"`
	AddedBy string  `json:"added_by"`
	AddedAt int64   `json:"added_at"`
}

// CollectionItems is a page of a collection and its currently visible items,
// newest added first. When HasMore is true, NextBeforeAt and NextBeforeID
// supply the cursor for the next page.
type CollectionItems struct {
	Collection   Collection       `json:"collection"`
	Items        []CollectionItem `json:"items"`
	HasMore      bool             `json:"has_more,omitempty"`
	NextBeforeAt int64            `json:"next_before_at,omitempty"`
	NextBeforeID string           `json:"next_before_id,omitempty"`
}

// CollectionItemRequest adds a message to a collection.
type CollectionItemRequest struct {
	MessageID string `json:"message_id"`
}

// CollectionDeleted names a deleted collection, and its channel if shared.
type CollectionDeleted struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id,omitempty"`
}

// CollectionItemEvent announces an item added to or removed from a collection,
// carrying its updated visible item count.
type CollectionItemEvent struct {
	CollectionID string `json:"collection_id"`
	ChannelID    string `json:"channel_id,omitempty"`
	MessageID    string `json:"message_id"`
	AddedBy      string `json:"added_by,omitempty"`
	AddedAt      int64  `json:"added_at,omitempty"`
	Count        int    `json:"count"`
}

// CollectionList holds the collections visible to the caller.
type CollectionList struct {
	Collections []Collection `json:"collections"`
}

// MessageCollections holds the IDs of the caller's accessible collections that
// currently hold a message.
type MessageCollections struct {
	CollectionIDs []string `json:"collection_ids"`
}

// CleanCollectionName strips hidden characters, keeps Unicode emoji whole and
// checks length (1 to 50 runes).
func CleanCollectionName(raw string) (string, error) {
	if len(raw) > maxCollectionNameBytes {
		return "", fmt.Errorf("a name is at most %d characters", MaxCollectionNameRunes)
	}
	return cleanText(raw, MaxCollectionNameRunes, true, "name")
}

// CheckWhoAdds checks a shared collection's who_adds setting.
func CheckWhoAdds(v string) error {
	if v != CollectionAddEveryone && v != CollectionAddStaff {
		return errors.New("who_adds must be everyone or staff")
	}
	return nil
}

// CheckCollection checks a collection from a den before a client keeps it.
func CheckCollection(c Collection) error {
	if _, err := ParseID(c.ID); err != nil {
		return errors.New("collection has an invalid ID")
	}
	clean, err := CleanCollectionName(c.Name)
	if err != nil || clean != c.Name {
		return errors.New("collection has an invalid name")
	}
	if c.ChannelID != "" {
		if _, err := ParseID(c.ChannelID); err != nil {
			return errors.New("collection has an invalid channel ID")
		}
		if err := CheckWhoAdds(c.WhoAdds); err != nil {
			return errors.New("collection has an invalid who_adds")
		}
	} else if c.WhoAdds != "" {
		return errors.New("personal collection must not set who_adds")
	}
	if c.CreatedBy != "" {
		if _, err := ParseID(c.CreatedBy); err != nil {
			return errors.New("collection has an invalid creator ID")
		}
	}
	if c.Count < 0 || c.Count > MaxCollectionItems {
		return errors.New("collection has an invalid item count")
	}
	if c.CreatedAt < 0 {
		return errors.New("collection has an invalid created_at")
	}
	return nil
}

// CheckCollectionItem checks a collection item from a den before a client uses it.
func CheckCollectionItem(item CollectionItem) error {
	if err := CheckMessage(item.Message); err != nil {
		return err
	}
	if _, err := ParseID(item.AddedBy); err != nil {
		return errors.New("collection item has an invalid added_by")
	}
	if item.AddedAt < 0 {
		return errors.New("collection item has an invalid added_at")
	}
	return nil
}

// CheckCollectionItemEvent checks a collection item event from a den.
func CheckCollectionItemEvent(ev CollectionItemEvent) error {
	if _, err := ParseID(ev.CollectionID); err != nil {
		return errors.New("collection item event has an invalid collection ID")
	}
	if ev.ChannelID != "" {
		if _, err := ParseID(ev.ChannelID); err != nil {
			return errors.New("collection item event has an invalid channel ID")
		}
	}
	if _, err := ParseID(ev.MessageID); err != nil {
		return errors.New("collection item event has an invalid message ID")
	}
	if ev.AddedBy != "" {
		if _, err := ParseID(ev.AddedBy); err != nil {
			return errors.New("collection item event has an invalid added_by")
		}
	}
	if ev.AddedAt < 0 {
		return errors.New("collection item event has an invalid added_at")
	}
	if ev.Count < 0 || ev.Count > MaxCollectionItems {
		return errors.New("collection item event has an invalid count")
	}
	return nil
}
