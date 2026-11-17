package store

import (
	"database/sql"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
)

// Tags 返回全部标签及数量。
func (s *Store) Tags() ([]model.Collection, error) {
	rows, err := s.db.Query(`SELECT t.id, COALESCE(t.name,''), COALESCE(t.color,''),
		(SELECT COUNT(*) FROM item_tags it WHERE it.tag_id = t.id)
		FROM tags t ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Collection
	for rows.Next() {
		var c model.Collection
		if err := rows.Scan(&c.ID, &c.Name, &c.Color, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TagsForToken 返回某条数据所属标签 id 集合。
func (s *Store) TagsForToken(token string) (map[int64]bool, error) {
	rows, err := s.db.Query("SELECT tag_id FROM item_tags WHERE token = ?", token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			out[id] = true
		}
	}
	return out, rows.Err()
}

// TagTokens 返回标签内的 token。
func (s *Store) TagTokens(id int64) ([]string, error) {
	rows, err := s.db.Query("SELECT token FROM item_tags WHERE tag_id = ? ORDER BY added_at DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err == nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

func (s *Store) CreateTag(name, color string) (int64, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := s.db.Exec("INSERT INTO tags (name, color, created_at, updated_at) VALUES (?,?,?,?)",
		name, color, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTag(id int64, name, color string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if name != "" {
		if _, err := s.db.Exec("UPDATE tags SET name = ?, updated_at = ? WHERE id = ?", name, now, id); err != nil {
			return err
		}
	}
	if color != "" {
		if _, err := s.db.Exec("UPDATE tags SET color = ?, updated_at = ? WHERE id = ?", color, now, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteTag(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM item_tags WHERE tag_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM tags WHERE id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddToTag(id int64, tokens []string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tokens {
		if _, err := tx.Exec("INSERT INTO item_tags (tag_id, token, added_at) VALUES (?,?,?) ON CONFLICT(tag_id, token) DO NOTHING", id, t, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RemoveFromTag(id int64, tokens []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tokens {
		if _, err := tx.Exec("DELETE FROM item_tags WHERE tag_id = ? AND token = ?", id, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) MergeTag(source, target int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO item_tags(tag_id, token, added_at)
		SELECT ?, token, added_at FROM item_tags WHERE tag_id = ?`, target, source); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM item_tags WHERE tag_id = ?", source); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM tags WHERE id = ?", source); err != nil {
		return err
	}
	return tx.Commit()
}

var _ = sql.ErrNoRows
