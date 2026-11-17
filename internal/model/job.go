package model

// TorrentJob 是一个磁力链接对应的下载任务（一个 infohash 一个 Job）。
type TorrentJob struct {
	ID         string           `json:"id"`
	Infohash   string           `json:"infohash"`
	Magnet     string           `json:"magnet"`
	Title      string           `json:"title"`
	SaveDir    string           `json:"saveDir"`
	State      string           `json:"state"`
	TotalBytes int64            `json:"totalBytes"`
	Selected   int              `json:"selected"`
	Completed  int              `json:"completed"`
	CreatedAt  string           `json:"createdAt"`
	UpdatedAt  string           `json:"updatedAt"`
	Peers      int              `json:"peers,omitempty"`
	Seeders    int              `json:"seeders,omitempty"`
	Files      []TorrentJobFile `json:"files,omitempty"`
}

// TorrentJobFile 是 Job 里用户勾选的一个文件。
type TorrentJobFile struct {
	JobID      string `json:"-"`
	Index      int    `json:"index"`
	Path       string `json:"path"`
	Length     int64  `json:"length"`
	Selected   bool   `json:"selected"`
	State      string `json:"state"` // queued | downloading | done | failed
	Bytes      int64  `json:"bytes"`
	Error      string `json:"error"`
	MediaToken string `json:"mediaToken,omitempty"`
}
