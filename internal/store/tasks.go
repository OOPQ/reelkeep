package store

import (
	"database/sql"
	"encoding/json"

	"github.com/OOPQ/reelkeep/internal/model"
)

// LoadTasks 读取全部下载任务（按加入时间）。
func (s *Store) LoadTasks() ([]model.DownloadTask, error) {
	rows, err := s.db.Query(`
		SELECT COALESCE(token,''), COALESCE(source,''), COALESCE(source_id,''),
		       COALESCE(url,''), COALESCE(mode,'http'), COALESCE(files,'[]'), COALESCE(headers,''),
		       COALESCE(use_auth,0), COALESCE(refresh_on_410,0), COALESCE(state,'queued'),
		       COALESCE(bytes,0), COALESCE(total,0), COALESCE(error,''),
		       COALESCE(added_at,''), COALESCE(started_at,''), COALESCE(finished_at,''),
		       COALESCE(history,0)
		FROM download_tasks ORDER BY added_at, token`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DownloadTask
	for rows.Next() {
		var t model.DownloadTask
		var useAuth, refresh, history int
		var files string
		if err := rows.Scan(&t.Token, &t.Source, &t.SourceID, &t.URL, &t.Mode, &files, &t.Headers,
			&useAuth, &refresh, &t.State, &t.Bytes, &t.Total, &t.Error,
			&t.AddedAt, &t.StartedAt, &t.FinishedAt, &history); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(files), &t.Files)
		t.UseAuth = useAuth != 0
		t.RefreshOn410 = refresh != 0
		t.History = history != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveTask 写入/更新一条任务。
func (s *Store) SaveTask(t model.DownloadTask) error {
	files, _ := json.Marshal(t.Files)
	if len(files) == 0 {
		files = []byte("[]")
	}
	_, err := s.db.Exec(`
		INSERT INTO download_tasks
		  (token, source, source_id, url, mode, files, headers, use_auth, refresh_on_410, state,
		   bytes, total, error, added_at, started_at, finished_at, history)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(token) DO UPDATE SET
		  source=excluded.source, source_id=excluded.source_id, url=excluded.url,
		  mode=excluded.mode, files=excluded.files, headers=excluded.headers, use_auth=excluded.use_auth,
		  refresh_on_410=excluded.refresh_on_410, state=excluded.state,
		  bytes=excluded.bytes, total=excluded.total, error=excluded.error,
		  started_at=excluded.started_at, finished_at=excluded.finished_at,
		  history=excluded.history`,
		t.Token, t.Source, t.SourceID, t.URL, t.Mode, string(files), t.Headers,
		boolInt(t.UseAuth), boolInt(t.RefreshOn410), t.State,
		t.Bytes, t.Total, t.Error, t.AddedAt, t.StartedAt, t.FinishedAt, boolInt(t.History))
	return err
}

// DeleteTask 删除一条任务记录。
func (s *Store) DeleteTask(token string) error {
	_, err := s.db.Exec("DELETE FROM download_tasks WHERE token = ?", token)
	return err
}

// ClearDoneTasks 清理已完成的队列历史，返回删除条数。
func (s *Store) ClearDoneTasks() (int64, error) {
	res, err := s.db.Exec("DELETE FROM download_tasks WHERE state IN ('done','cancelled')")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountTasks 返回任务总数（迁移判断用）。
func (s *Store) CountTasks() int {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM download_tasks").Scan(&n); err != nil && err != sql.ErrNoRows {
		return 0
	}
	return n
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
