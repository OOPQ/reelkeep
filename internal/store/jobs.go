package store

import (
	"database/sql"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
)

// SaveJob 写入/合并一个磁力任务及其文件选择。
func (s *Store) SaveJob(j model.TorrentJob) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if j.CreatedAt == "" {
		j.CreatedAt = now
	}
	j.UpdatedAt = now
	var selected int
	var total int64
	for _, f := range j.Files {
		if f.Selected {
			selected++
			total += f.Length
		}
	}
	j.Selected = selected
	j.TotalBytes = total
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO torrent_jobs
		(id, infohash, magnet, title, save_dir, state, total_bytes, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  magnet=excluded.magnet, title=excluded.title, save_dir=excluded.save_dir,
		  total_bytes=excluded.total_bytes, updated_at=excluded.updated_at`,
		j.ID, j.Infohash, j.Magnet, j.Title, j.SaveDir, j.State, total, j.CreatedAt, j.UpdatedAt); err != nil {
		return err
	}
	for _, f := range j.Files {
		selected := 0
		if f.Selected {
			selected = 1
		}
		if _, err := tx.Exec(`INSERT INTO torrent_job_files
			(job_id, file_index, path, length, selected, state, bytes, error, media_token)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(job_id, file_index) DO UPDATE SET
			  path=excluded.path, length=excluded.length, selected=excluded.selected`,
			j.ID, f.Index, f.Path, f.Length, selected, f.State, f.Bytes, f.Error, f.MediaToken); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetJob 读取一个任务及文件。
func (s *Store) GetJob(id string) (model.TorrentJob, bool, error) {
	var j model.TorrentJob
	err := s.db.QueryRow(`SELECT COALESCE(id,''), COALESCE(infohash,''), COALESCE(magnet,''),
		COALESCE(title,''), COALESCE(save_dir,''), COALESCE(state,'queued'),
		COALESCE(total_bytes,0), COALESCE(created_at,''), COALESCE(updated_at,'')
		FROM torrent_jobs WHERE id = ?`, id).Scan(
		&j.ID, &j.Infohash, &j.Magnet, &j.Title, &j.SaveDir, &j.State,
		&j.TotalBytes, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return j, false, nil
	}
	if err != nil {
		return j, false, err
	}
	files, err := s.JobFiles(id)
	if err != nil {
		return j, false, err
	}
	j.Files = files
	for _, f := range files {
		if f.Selected {
			j.Selected++
		}
		if f.State == "done" {
			j.Completed++
		}
	}
	return j, true, nil
}

// JobFiles 读取任务文件。
func (s *Store) JobFiles(id string) ([]model.TorrentJobFile, error) {
	rows, err := s.db.Query(`SELECT COALESCE(job_id,''), COALESCE(file_index,0), COALESCE(path,''),
		COALESCE(length,0), COALESCE(selected,1), COALESCE(state,'queued'),
		COALESCE(bytes,0), COALESCE(error,''), COALESCE(media_token,'')
		FROM torrent_job_files WHERE job_id = ? ORDER BY file_index`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TorrentJobFile
	for rows.Next() {
		var f model.TorrentJobFile
		var selected int
		if err := rows.Scan(&f.JobID, &f.Index, &f.Path, &f.Length, &selected, &f.State, &f.Bytes, &f.Error, &f.MediaToken); err != nil {
			return nil, err
		}
		f.Selected = selected != 0
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListJobs 读取所有任务，按创建时间倒序。
func (s *Store) ListJobs() ([]model.TorrentJob, error) {
	rows, err := s.db.Query("SELECT id FROM torrent_jobs ORDER BY created_at DESC, id")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := make([]model.TorrentJob, 0, len(ids))
	for _, id := range ids {
		j, ok, err := s.GetJob(id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, j)
		}
	}
	return out, nil
}

// UpdateJobState 更新任务状态。
func (s *Store) UpdateJobState(id, state string) {
	_, _ = s.db.Exec("UPDATE torrent_jobs SET state = ?, updated_at = ? WHERE id = ?",
		state, time.Now().Format("2006-01-02 15:04:05"), id)
}

// UpdateJobFile 更新单个文件的下载状态。
func (s *Store) UpdateJobFile(jobID string, index int, state string, bytes int64, errText, mediaToken string) {
	_, _ = s.db.Exec(`UPDATE torrent_job_files SET state = ?, bytes = ?, error = ?, media_token = ?
		WHERE job_id = ? AND file_index = ?`, state, bytes, errText, mediaToken, jobID, index)
}

// DeleteJob 删除任务及其文件记录，不动媒体文件。
func (s *Store) DeleteJob(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM torrent_job_files WHERE job_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM torrent_jobs WHERE id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}
