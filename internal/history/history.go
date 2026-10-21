// Package history keeps the list of fetched links in a bbolt file.
package history

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

type FetchItem struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Info      string `json:"info"`
	Image     string `json:"image"`
	Data      string `json:"data"`
	Timestamp int64  `json:"timestamp"`
}

const (
	bucket = "FetchHistory"
	// Each item carries the whole fetched JSON (a playlist can be megabytes).
	maxItems = 200
)

var ErrNotFound = errors.New("history item not found")

type DB struct{ db *bolt.DB }

// Open locks path, so a second process fails after the 1 s timeout.
func Open(path string) (*DB, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// Add stores item under type|url, so a re-fetch replaces the old entry,
// and drops the oldest entries beyond maxItems.
func (d *DB) Add(item FetchItem) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		item.ID = item.Type + "|" + item.URL
		item.Timestamp = time.Now().Unix()
		buf, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(item.ID), buf); err != nil {
			return err
		}
		// Stats().KeyN is stale inside a write transaction, so count keys directly.
		n := 0
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			n++
		}
		if n <= maxItems {
			return nil
		}
		return trim(b)
	})
}

// trim deletes the oldest entries beyond maxItems.
func trim(b *bolt.Bucket) error {
	type entry struct {
		key []byte
		ts  int64
	}
	var all []entry
	err := b.ForEach(func(k, v []byte) error {
		var item FetchItem
		_ = json.Unmarshal(v, &item) // unreadable entries get ts 0 and go first
		all = append(all, entry{append([]byte(nil), k...), item.Timestamp})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ts > all[j].ts })
	for _, e := range all[maxItems:] {
		if err := b.Delete(e.key); err != nil {
			return err
		}
	}
	return nil
}

// List returns items newest first, without Data: the payloads are large and
// the list is read on every Home visit. Use Data to load one.
func (d *DB) List() ([]FetchItem, error) {
	items := []FetchItem{}
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var item FetchItem
			if json.Unmarshal(v, &item) == nil {
				item.Data = ""
				items = append(items, item)
			}
			return nil
		})
	})
	sort.Slice(items, func(i, j int) bool { return items[i].Timestamp > items[j].Timestamp })
	return items, err
}

// Data returns the stored payload of the item with the given id.
func (d *DB) Data(id string) (string, error) {
	var data string
	err := d.db.View(func(tx *bolt.Tx) error {
		var v []byte
		if b := tx.Bucket([]byte(bucket)); b != nil {
			v = b.Get([]byte(id))
		}
		if v == nil {
			return ErrNotFound
		}
		var item FetchItem
		if err := json.Unmarshal(v, &item); err != nil {
			return err
		}
		data = item.Data
		return nil
	})
	return data, err
}

func (d *DB) Delete(id string) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		if b := tx.Bucket([]byte(bucket)); b != nil {
			return b.Delete([]byte(id))
		}
		return nil
	})
}

func (d *DB) Clear() error {
	return d.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte(bucket)) == nil {
			return nil
		}
		return tx.DeleteBucket([]byte(bucket))
	})
}

func (d *DB) ClearByType(itemType string) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		var keys [][]byte
		err := b.ForEach(func(k, v []byte) error {
			var item FetchItem
			if json.Unmarshal(v, &item) == nil && item.Type == itemType {
				keys = append(keys, append([]byte(nil), k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}
