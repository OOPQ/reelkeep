// Package store 是台账的事实来源：SQLite（WAL）。
//
// 表和列与原版（PowerShell + System.Data.SQLite）完全一致，只允许「加列」，
// 不允许改名/改类型 —— 旧数据、ledger.csv 和前端都依赖这套契约。
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/OOPQ/reelkeep/internal/jsonx"
	"github.com/OOPQ/reelkeep/internal/model"
)

// Store 包装一个 SQLite 连接。
type Store struct {
	db   *sql.DB
	cols string // "COALESCE(token,''),..."
}

// Open 打开（必要时创建）数据库。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单写者：SQLite + WAL 下串行化最省心，也避免「database is locked」。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	s.cols = strings.Join(quotedCoalesce(), ", ")
	if err := s.rebuildFTSIfNeeded(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) rebuildFTSIfNeeded() error {
	var items, known int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM items").Scan(&items); err != nil {
		return err
	}
	_ = s.db.QueryRow("SELECT COALESCE((SELECT items FROM fts_meta LIMIT 1), -1)").Scan(&known)
	if items == known {
		return nil
	}
	if _, err := s.db.Exec("INSERT INTO items_fts(items_fts) VALUES('rebuild')"); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM fts_meta")
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO fts_meta(items, rebuilt_at) VALUES(?, datetime('now'))", items)
	return err
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// DB 暴露底层连接（给确实需要 SELECT 的包用）。
func (s *Store) DB() *sql.DB { return s.db }

func quotedCoalesce() []string {
	out := make([]string, len(model.Columns))
	for i, c := range model.Columns {
		out[i] = "COALESCE(" + c + ", '')"
	}
	return out
}

const schema = `CREATE TABLE IF NOT EXISTS items (
  token      TEXT PRIMARY KEY,
  status     TEXT DEFAULT 'new',
  fav        TEXT DEFAULT '',
  rank       TEXT DEFAULT '',
  page       TEXT DEFAULT '',
  duration   TEXT DEFAULT '',
  seconds    TEXT DEFAULT '',
  res        TEXT DEFAULT '',
  views      TEXT DEFAULT '',
  likes      TEXT DEFAULT '',
  uploaded   TEXT DEFAULT '',
  url        TEXT DEFAULT '',
  page_url   TEXT DEFAULT '',
  site       TEXT DEFAULT 'pektino',
  source     TEXT DEFAULT 'pektino',
  source_id  TEXT DEFAULT '',
  title      TEXT DEFAULT '',
  cover_url  TEXT DEFAULT '',
  download_kind TEXT DEFAULT 'http',
  download_headers TEXT DEFAULT '',
  extra      TEXT DEFAULT '',
  first_seen TEXT DEFAULT '',
  last_seen  TEXT DEFAULT '',
  sent_at    TEXT DEFAULT '',
  note       TEXT DEFAULT '',
  file_name  TEXT DEFAULT '',
  file_path  TEXT DEFAULT '',
  file_size  TEXT DEFAULT '',
  file_date  TEXT DEFAULT '',
  play_count TEXT DEFAULT '',
  last_play  TEXT DEFAULT '',
  rating     TEXT DEFAULT '',
  recycled_at TEXT DEFAULT '',
  recycled_from TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_items_status ON items(status);
CREATE INDEX IF NOT EXISTS idx_items_fav ON items(fav);
CREATE INDEX IF NOT EXISTS idx_items_source ON items(source);
CREATE INDEX IF NOT EXISTS idx_items_first_seen ON items(first_seen);
CREATE INDEX IF NOT EXISTS idx_items_last_play ON items(last_play);
CREATE INDEX IF NOT EXISTS idx_items_rating ON items(rating);
CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
  token, title, source, site, note,
  content='items', content_rowid='rowid', tokenize='trigram'
);
CREATE TABLE IF NOT EXISTS deleted_tokens (
  token      TEXT PRIMARY KEY,
  source     TEXT DEFAULT '',
  title      TEXT DEFAULT '',
  deleted_at TEXT DEFAULT ''
);
CREATE TABLE IF NOT EXISTS collections (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL,
  color      TEXT DEFAULT '',
  created_at TEXT DEFAULT '',
  updated_at TEXT DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_collections_name ON collections(name COLLATE NOCASE);
CREATE TABLE IF NOT EXISTS collection_items (
  collection_id INTEGER NOT NULL,
  token         TEXT NOT NULL,
  added_at      TEXT DEFAULT '',
  PRIMARY KEY (collection_id, token)
);
CREATE INDEX IF NOT EXISTS idx_collection_items_token ON collection_items(token);
CREATE TABLE IF NOT EXISTS audit_log (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  at      TEXT DEFAULT '',
  action  TEXT DEFAULT '',
  token   TEXT DEFAULT '',
  detail  TEXT DEFAULT ''
);
CREATE TABLE IF NOT EXISTS download_tasks (
  token          TEXT PRIMARY KEY,
  source         TEXT DEFAULT '',
  source_id      TEXT DEFAULT '',
  url            TEXT DEFAULT '',
  mode           TEXT DEFAULT 'http',
  files          TEXT DEFAULT '[]',
  name           TEXT DEFAULT '',
  headers        TEXT DEFAULT '',
  use_auth       INTEGER DEFAULT 0,
  refresh_on_410 INTEGER DEFAULT 0,
  state          TEXT DEFAULT 'queued',
  bytes          INTEGER DEFAULT 0,
  total          INTEGER DEFAULT 0,
  error          TEXT DEFAULT '',
  added_at       TEXT DEFAULT '',
  started_at     TEXT DEFAULT '',
  finished_at    TEXT DEFAULT '',
  history        INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_download_state ON download_tasks(state);
CREATE TABLE IF NOT EXISTS torrent_jobs (
  id           TEXT PRIMARY KEY,
  infohash     TEXT DEFAULT '',
  magnet       TEXT DEFAULT '',
  title        TEXT DEFAULT '',
  save_dir     TEXT DEFAULT '',
  state        TEXT DEFAULT 'queued',
  total_bytes  INTEGER DEFAULT 0,
  created_at   TEXT DEFAULT '',
  updated_at   TEXT DEFAULT ''
);
CREATE TABLE IF NOT EXISTS torrent_job_files (
  job_id       TEXT NOT NULL,
  file_index   INTEGER NOT NULL,
  path         TEXT DEFAULT '',
  length       INTEGER DEFAULT 0,
  selected     INTEGER DEFAULT 1,
  state        TEXT DEFAULT 'queued',
  bytes        INTEGER DEFAULT 0,
  error        TEXT DEFAULT '',
  media_token  TEXT DEFAULT '',
  PRIMARY KEY (job_id, file_index)
);
CREATE INDEX IF NOT EXISTS idx_job_files_job ON torrent_job_files(job_id);
CREATE TABLE IF NOT EXISTS fts_meta (
  items      INTEGER DEFAULT 0,
  rebuilt_at TEXT DEFAULT ''
);
`

var ftsTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS items_fts_ai AFTER INSERT ON items BEGIN
  INSERT INTO items_fts(rowid, token, title, source, site, note)
  VALUES (new.rowid, new.token, new.title, new.source, new.site, new.note);
END;`,
	`CREATE TRIGGER IF NOT EXISTS items_fts_ad AFTER DELETE ON items BEGIN
  INSERT INTO items_fts(items_fts, rowid, token, title, source, site, note)
  VALUES ('delete', old.rowid, old.token, old.title, old.source, old.site, old.note);
END;`,
	`CREATE TRIGGER IF NOT EXISTS items_fts_au AFTER UPDATE OF token, title, source, site, note ON items BEGIN
  INSERT INTO items_fts(items_fts, rowid, token, title, source, site, note)
  VALUES ('delete', old.rowid, old.token, old.title, old.source, old.site, old.note);
  INSERT INTO items_fts(rowid, token, title, source, site, note)
  VALUES (new.rowid, new.token, new.title, new.source, new.site, new.note);
END;`,
}

func (s *Store) ensureSchema() error {
	for _, stmt := range strings.Split(schema, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("建表失败: %w", err)
		}
	}
	for _, stmt := range ftsTriggers {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("建全文索引失败: %w", err)
		}
	}
	// 老库补列：collections.color
	has := map[string]bool{}
	rows, err := s.db.Query("PRAGMA table_info(collections)")
	if err == nil {
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt sql.NullString
			if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil {
				has[name] = true
			}
		}
		rows.Close()
	}
	if !has["color"] {
		if _, err := s.db.Exec("ALTER TABLE collections ADD COLUMN color TEXT DEFAULT ''"); err != nil {
			return err
		}
	}
	// 老库补列：download_tasks.files（磁力文件选择）
	has = map[string]bool{}
	rows, err = s.db.Query("PRAGMA table_info(download_tasks)")
	if err == nil {
		for rows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt sql.NullString
			if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil {
				has[name] = true
			}
		}
		rows.Close()
	}
	if !has["files"] {
		if _, err := s.db.Exec("ALTER TABLE download_tasks ADD COLUMN files TEXT DEFAULT '[]'"); err != nil {
			return err
		}
	}
	if !has["name"] {
		if _, err := s.db.Exec("ALTER TABLE download_tasks ADD COLUMN name TEXT DEFAULT ''"); err != nil {
			return err
		}
	}
	return nil
}

// ClearHistory 清空播放历史字段，不影响文件。
func (s *Store) ClearHistory() error {
	_, err := s.db.Exec("UPDATE items SET last_play = '', play_count = '' WHERE last_play <> '' OR play_count <> ''")
	return err
}

// LoadRows 按插入顺序读取全部台账。
func (s *Store) LoadRows() ([]model.Row, error) {
	q := "SELECT " + s.cols + " FROM items ORDER BY rowid"
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.Row, 0, 2048)
	for rows.Next() {
		var r model.Row
		if err := rows.Scan(r.Ptrs()...); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertRows 只写变化行（扫描每几秒触发一次，整表 upsert 是主要卡顿源）。
func (s *Store) UpsertRows(list []model.Row) error {
	if len(list) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(model.Columns)), ",")
	var setParts []string
	for _, c := range model.Columns {
		if c == "token" {
			continue
		}
		setParts = append(setParts, c+"=excluded."+c)
	}
	q := "INSERT INTO items (" + strings.Join(model.Columns, ",") + ") VALUES (" + placeholders +
		") ON CONFLICT(token) DO UPDATE SET " + strings.Join(setParts, ", ")
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := range list {
		if _, err := stmt.Exec(list[i].Values()...); err != nil {
			return fmt.Errorf("写入 %s: %w", list[i].Token, err)
		}
	}
	return tx.Commit()
}

// GetRow 按 token 精确读取。
func (s *Store) GetRow(token string) (model.Row, bool, error) {
	var r model.Row
	q := "SELECT " + s.cols + " FROM items WHERE token = ?"
	err := s.db.QueryRow(q, token).Scan(r.Ptrs()...)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	return r, err == nil, err
}

// ---------- 单字段更新 ----------

func (s *Store) execForTokens(q string, tokens []string, args ...any) error {
	if len(tokens) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range tokens {
		a := append(append([]any{}, args...), t)
		if _, err := stmt.Exec(a...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetFav 收藏/取消收藏。
func (s *Store) SetFav(tokens []string, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return s.execForTokens("UPDATE items SET fav = ? WHERE token = ?", tokens, v)
}

// SetRating 打分（0 表示清除）。
func (s *Store) SetRating(tokens []string, value int) error {
	return s.execForTokens("UPDATE items SET rating = ? WHERE token = ?", tokens, fmt.Sprint(value))
}

// SetStatus 直接改状态（mark/已看/待看）。
func (s *Store) SetStatus(tokens []string, status string) error {
	q := "UPDATE items SET status = ? WHERE token = ?"
	if status == "watched" {
		q = "UPDATE items SET last_play = ? WHERE token = ?"
		return s.execForTokens(q, tokens, time.Now().Format("2006-01-02 15:04:05"))
	}
	return s.execForTokens(q, tokens, status)
}

// SetWatched 标记已看/未看（未看清空 last_play，与旧版一致）。
func (s *Store) SetWatched(tokens []string, on bool) error {
	if on {
		return s.execForTokens("UPDATE items SET last_play = ? WHERE token = ?", tokens,
			time.Now().Format("2006-01-02 15:04:05"))
	}
	return s.execForTokens("UPDATE items SET last_play = '' WHERE token = ?", tokens)
}

// Trash 移入回收站：记录原状态，恢复时回滚。
func (s *Store) Trash(tokens []string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tokens {
		if _, err := tx.Exec(
			"UPDATE items SET recycled_from = CASE WHEN status = 'recycled' THEN recycled_from ELSE status END, "+
				"status = 'recycled', recycled_at = ? WHERE token = ?", now, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Restore 从回收站恢复：回到原状态（缺省 new）。
func (s *Store) Restore(tokens []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tokens {
		if _, err := tx.Exec(
			"UPDATE items SET status = CASE WHEN recycled_from = '' THEN 'new' ELSE recycled_from END, "+
				"recycled_at = '', recycled_from = '' WHERE token = ?", t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemovePermanently 彻底删除台账行（文件删除由调用方负责）。
func (s *Store) RemovePermanently(tokens []string, deleted []model.DeletedToken) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tokens {
		if _, err := tx.Exec("DELETE FROM items WHERE token = ?", t); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM collection_items WHERE token = ?", t); err != nil {
			return err
		}
	}
	for _, d := range deleted {
		if _, err := tx.Exec(
			"INSERT INTO deleted_tokens (token, source, title, deleted_at) VALUES (?,?,?,?) "+
				"ON CONFLICT(token) DO UPDATE SET source=excluded.source, title=excluded.title, deleted_at=excluded.deleted_at",
			d.Token, d.Source, d.Title, d.DeletedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- 彻底删除记录 ----------

// LoadDeleted 合并 deleted.json 和 deleted_tokens 表。
func (s *Store) LoadDeleted(dataDir string) map[string]model.DeletedToken {
	out := map[string]model.DeletedToken{}
	if b, err := os.ReadFile(filepath.Join(dataDir, "deleted.json")); err == nil {
		var list []model.DeletedToken
		if jsonx.Unmarshal(b, &list) == nil {
			for _, d := range list {
				if d.Token != "" {
					out[strings.ToLower(d.Token)] = d
				}
			}
		}
	}
	rows, err := s.db.Query("SELECT COALESCE(token,''), COALESCE(source,''), COALESCE(title,''), COALESCE(deleted_at,'') FROM deleted_tokens")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var d model.DeletedToken
			if rows.Scan(&d.Token, &d.Source, &d.Title, &d.DeletedAt) == nil && d.Token != "" {
				out[strings.ToLower(d.Token)] = d
			}
		}
	}
	return out
}

// SaveDeletedJSON 把内存里的删除记录写回 deleted.json（原版格式：数组）。
func SaveDeletedJSON(dataDir string, m map[string]model.DeletedToken) error {
	list := make([]model.DeletedToken, 0, len(m))
	for _, d := range m {
		list = append(list, d)
	}
	b, _ := json.MarshalIndent(list, "", "  ")
	return os.WriteFile(filepath.Join(dataDir, "deleted.json"), b, 0o644)
}

// ---------- 合集 ----------

// Collections 返回全部合集（带条目数）。
func (s *Store) Collections() ([]model.Collection, error) {
	rows, err := s.db.Query(`
		SELECT c.id, COALESCE(c.name,''), COALESCE(c.color,''),
		       (SELECT COUNT(*) FROM collection_items ci WHERE ci.collection_id = c.id)
		FROM collections c ORDER BY c.id`)
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

// CollectionsForToken 返回某条目所属的合集 id 集合。
func (s *Store) CollectionsForToken(token string) (map[int64]bool, error) {
	rows, err := s.db.Query("SELECT collection_id FROM collection_items WHERE token = ?", token)
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

// CollectionTokens 返回合集内 token（按加入时间倒序）。
func (s *Store) CollectionTokens(id int64) ([]string, error) {
	rows, err := s.db.Query("SELECT token FROM collection_items WHERE collection_id = ? ORDER BY added_at DESC", id)
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

// CreateCollection 新建合集，返回 id。
func (s *Store) CreateCollection(name, color string) (int64, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := s.db.Exec(
		"INSERT INTO collections (name, color, created_at, updated_at) VALUES (?,?,?,?)",
		strings.TrimSpace(name), color, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCollection 改名/改色/无操作。
func (s *Store) UpdateCollection(id int64, name, color string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if name != "" {
		if _, err := s.db.Exec("UPDATE collections SET name = ?, updated_at = ? WHERE id = ?", name, now, id); err != nil {
			return err
		}
	}
	if color != "" {
		if _, err := s.db.Exec("UPDATE collections SET color = ?, updated_at = ? WHERE id = ?", color, now, id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCollection 删除合集及其关联。
func (s *Store) DeleteCollection(id int64) error {
	if _, err := s.db.Exec("DELETE FROM collection_items WHERE collection_id = ?", id); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM collections WHERE id = ?", id)
	return err
}

// AddToCollection 把条目加入合集（幂等）。
func (s *Store) AddToCollection(id int64, tokens []string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tokens {
		if _, err := tx.Exec(
			"INSERT INTO collection_items (collection_id, token, added_at) VALUES (?,?,?) "+
				"ON CONFLICT(collection_id, token) DO NOTHING", id, t, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveFromCollection 移出合集。
func (s *Store) RemoveFromCollection(id int64, tokens []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tokens {
		if _, err := tx.Exec("DELETE FROM collection_items WHERE collection_id = ? AND token = ?", id, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Audit 写一条审计记录（失败不影响主流程）。
func (s *Store) Audit(action, token, detail string) {
	_, _ = s.db.Exec("INSERT INTO audit_log (at, action, token, detail) VALUES (?,?,?,?)",
		time.Now().Format("2006-01-02 15:04:05"), action, token, detail)
}
