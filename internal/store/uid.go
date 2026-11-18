package store

import "github.com/OOPQ/reelkeep/internal/model"

func (s *Store) ensureUIDColumns() error {
	alter := func(table, column, decl string) error {
		rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return err
		}
		has := false
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt any
			if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil && name == column {
				has = true
			}
		}
		rows.Close()
		if !has {
			_, err = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + decl)
		}
		return err
	}
	if err := alter("items", "uid", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := alter("collection_items", "item_uid", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := alter("item_tags", "item_uid", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := alter("download_tasks", "item_uid", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

// backfillUIDs assigns the stable media identity and links existing rows.
func (s *Store) backfillUIDs() error {
	rows, err := s.db.Query("SELECT token, COALESCE(source,''), COALESCE(source_id,'') FROM items")
	if err != nil {
		return err
	}
	type item struct{ token, source, sourceID string }
	var list []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.token, &it.source, &it.sourceID) == nil {
			list = append(list, it)
		}
	}
	rows.Close()
	for _, it := range list {
		uid := model.MediaUID(it.source, it.sourceID, it.token)
		if uid == "" {
			continue
		}
		if _, err := s.db.Exec("UPDATE items SET uid = ? WHERE token = ?", uid, it.token); err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		"UPDATE collection_items SET item_uid = COALESCE((SELECT uid FROM items WHERE items.token = collection_items.token), '') WHERE item_uid = ''",
		"UPDATE item_tags SET item_uid = COALESCE((SELECT uid FROM items WHERE items.token = item_tags.token), '') WHERE item_uid = ''",
		"UPDATE download_tasks SET item_uid = COALESCE((SELECT uid FROM items WHERE items.token = download_tasks.token), '') WHERE item_uid = ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_items_uid ON items(uid) WHERE uid <> ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_download_tasks_uid ON download_tasks(item_uid) WHERE item_uid <> ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_collection_items_uid ON collection_items(collection_id, item_uid) WHERE item_uid <> ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_item_tags_uid ON item_tags(tag_id, item_uid) WHERE item_uid <> ''",
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
