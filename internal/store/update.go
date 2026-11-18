package store

// 这里放「只改几列」的定向更新：下载回填、播放计数这类高频写操作
// 不能整行 upsert，否则会和并发的其它修改互相覆盖。

// SetDownloadedFile 写回下载完成后的文件字段（token 不存在时无操作）。
func (s *Store) SetDownloadedFile(token, name, path, size, date, title string) error {
	if title != "" {
		_, err := s.db.Exec(`UPDATE items SET file_name=?, file_path=?, file_size=?, file_date=?,
			status='done', note='auto:download', title=? WHERE token=?`,
			name, path, size, date, title, token)
		return err
	}
	_, err := s.db.Exec(`UPDATE items SET file_name=?, file_path=?, file_size=?, file_date=?,
		status='done', note='auto:download' WHERE token=?`, name, path, size, date, token)
	return err
}

// BumpPlayCount 原子累加播放次数并记录最近播放时间，返回新的次数。
func (s *Store) BumpPlayCount(token, at string) (string, error) {
	if _, err := s.db.Exec(`UPDATE items SET
		play_count = CAST(COALESCE(NULLIF(play_count,''),'0') AS INTEGER) + 1,
		last_play = ? WHERE token = ?`, at, token); err != nil {
		return "", err
	}
	var n string
	if err := s.db.QueryRow(`SELECT COALESCE(play_count,'') FROM items WHERE token = ?`, token).Scan(&n); err != nil {
		return "", err
	}
	return n, nil
}
