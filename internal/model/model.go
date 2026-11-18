package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Row 是台账的一行。字段顺序与 data.db 的 items 表、ledger.csv 表头完全一致，
// 34 列一个不多一个不少 —— 这是旧版（PowerShell）和前端共同依赖的硬契约。
type Row struct {
	Token           string   `json:"token"`
	Status          string   `json:"status"`
	Fav             string   `json:"fav"`
	Rank            string   `json:"rank"`
	Page            string   `json:"page"`
	Duration        string   `json:"duration"`
	Seconds         string   `json:"seconds"`
	Res             string   `json:"res"`
	Views           string   `json:"views"`
	Likes           string   `json:"likes"`
	Uploaded        string   `json:"uploaded"`
	URL             string   `json:"url"`
	PageURL         string   `json:"page_url"`
	Site            string   `json:"site"`
	Source          string   `json:"source"`
	SourceID        string   `json:"source_id"`
	Title           string   `json:"title"`
	CoverURL        string   `json:"cover_url"`
	DownloadKind    string   `json:"download_kind"`
	DownloadHeaders string   `json:"download_headers"`
	Extra           string   `json:"extra"`
	FirstSeen       string   `json:"first_seen"`
	LastSeen        string   `json:"last_seen"`
	SentAt          string   `json:"sent_at"`
	Note            string   `json:"note"`
	FileName        string   `json:"file_name"`
	FilePath        string   `json:"file_path"`
	FileSize        string   `json:"file_size"`
	FileDate        string   `json:"file_date"`
	PlayCount       string   `json:"play_count"`
	LastPlay        string   `json:"last_play"`
	Rating          string   `json:"rating"`
	RecycledAt      string   `json:"recycled_at"`
	RecycledFrom    string   `json:"recycled_from"`
	Tags            []TagRef `json:"tags,omitempty"`
}

// TagRef 是媒体库条目携带的标签。
type TagRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Columns 是 items 表的列名，顺序与 Row 字段一致。
// MediaUID returns a stable media identity independent of the mutable token.
// It is used for media filenames and survives source-token migrations.
func MediaUID(source, sourceID, token string) string {
	// Rows without a native id keep the legacy token-based filename.
	if strings.TrimSpace(sourceID) == "" {
		return strings.TrimSpace(token)
	}
	key := strings.ToLower(strings.TrimSpace(source)) + "\x00" + strings.TrimSpace(sourceID)
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

var Columns = []string{
	"token", "status", "fav", "rank", "page",
	"duration", "seconds", "res", "views", "likes",
	"uploaded", "url", "page_url", "site", "source",
	"source_id", "title", "cover_url", "download_kind", "download_headers",
	"extra", "first_seen", "last_seen", "sent_at", "note",
	"file_name", "file_path", "file_size", "file_date", "play_count",
	"last_play", "rating", "recycled_at", "recycled_from",
}

// Header 是 ledger.csv 的表头（与旧版逐字段一致）。
var Header = Columns

// Ptrs 返回 34 个字段的扫描目标，供 database/sql 按列顺序 Scan。
func (r *Row) Ptrs() []any {
	return []any{
		&r.Token, &r.Status, &r.Fav, &r.Rank, &r.Page,
		&r.Duration, &r.Seconds, &r.Res, &r.Views, &r.Likes,
		&r.Uploaded, &r.URL, &r.PageURL, &r.Site, &r.Source,
		&r.SourceID, &r.Title, &r.CoverURL, &r.DownloadKind, &r.DownloadHeaders,
		&r.Extra, &r.FirstSeen, &r.LastSeen, &r.SentAt, &r.Note,
		&r.FileName, &r.FilePath, &r.FileSize, &r.FileDate, &r.PlayCount,
		&r.LastPlay, &r.Rating, &r.RecycledAt, &r.RecycledFrom,
	}
}

// Values 返回 34 个字段的值，供 INSERT/UPDATE 使用。
func (r *Row) Values() []any {
	return []any{
		r.Token, r.Status, r.Fav, r.Rank, r.Page,
		r.Duration, r.Seconds, r.Res, r.Views, r.Likes,
		r.Uploaded, r.URL, r.PageURL, r.Site, r.Source,
		r.SourceID, r.Title, r.CoverURL, r.DownloadKind, r.DownloadHeaders,
		r.Extra, r.FirstSeen, r.LastSeen, r.SentAt, r.Note,
		r.FileName, r.FilePath, r.FileSize, r.FileDate, r.PlayCount,
		r.LastPlay, r.Rating, r.RecycledAt, r.RecycledFrom,
	}
}

// DeletedToken 是彻底删除记录（deleted.json / deleted_tokens 表）。
type DeletedToken struct {
	Token     string `json:"token"`
	Source    string `json:"source"`
	Title     string `json:"title"`
	DeletedAt string `json:"deletedAt"`
}

// Collection 是合集。
type Collection struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	CoverToken string `json:"coverToken,omitempty"`
	Count      int    `json:"count"`
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

// DownloadTask 是一条下载任务（持久化在 download_tasks 表）。
type DownloadTask struct {
	Token        string `json:"token"`
	Source       string `json:"source"`
	SourceID     string `json:"sourceId"`
	URL          string `json:"url"`
	Mode         string `json:"mode"` // http | hls | torrent
	Files        []int  `json:"files,omitempty"`
	Name         string `json:"name,omitempty"`
	Headers      string `json:"headers"` // JSON: {"Referer":"..."}
	UseAuth      bool   `json:"useAuth"`
	RefreshOn410 bool   `json:"refreshOn410"`
	State        string `json:"state"` // queued | downloading | paused | done | failed | cancelled
	Bytes        int64  `json:"bytes"`
	Total        int64  `json:"total"`
	Error        string `json:"error"`
	AddedAt      string `json:"addedAt"`
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
	History      bool   `json:"history"`
}
