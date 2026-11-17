package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OOPQ/reelkeep/internal/model"
)

type qbtClient struct {
	base string
	user string
	pass string
	http *http.Client
}

type qbtFile struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

type qbtInfo struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"`
	Size        int64   `json:"size"`
	Completed   int64   `json:"completed"`
	DlSpeed     int64   `json:"dlspeed"`
	NumSeeds    int     `json:"num_seeds"`
	NumLeechers int     `json:"num_leechs"`
	SavePath    string  `json:"save_path"`
	ContentPath string  `json:"content_path"`
}

func newQbt(base, user, pass string) (*qbtClient, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, errors.New("未配置 qBittorrent Web API 地址")
	}
	jar, _ := cookiejar.New(nil)
	return &qbtClient{base: base, user: user, pass: pass, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}, nil
}

func (q *qbtClient) login(ctx context.Context) error {
	form := url.Values{"username": {q.user}, "password": {q.pass}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := q.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接 qBittorrent 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Ok") {
		return fmt.Errorf("qBittorrent 登录失败: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func (q *qbtClient) addTorrent(ctx context.Context, magnet, savePath string) error {
	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("urls", magnet)
	_ = w.WriteField("savepath", savePath)
	_ = w.WriteField("category", "reelkeep")
	_ = w.WriteField("autoTMM", "false")
	_ = w.WriteField("paused", "false")
	_ = w.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/torrents/add", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := q.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != 200 {
		return fmt.Errorf("qBittorrent 添加任务失败: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (q *qbtClient) files(ctx context.Context, hash string) ([]qbtFile, error) {
	var out []qbtFile
	err := q.getJSON(ctx, "/api/v2/torrents/files?hash="+url.QueryEscape(hash), &out)
	return out, err
}

func (q *qbtClient) info(ctx context.Context, hash string) (qbtInfo, error) {
	var out []qbtInfo
	err := q.getJSON(ctx, "/api/v2/torrents/info?hashes="+url.QueryEscape(hash), &out)
	if err != nil {
		return qbtInfo{}, err
	}
	if len(out) == 0 {
		return qbtInfo{}, errors.New("qBittorrent 中找不到该任务")
	}
	return out[0], nil
}

func (q *qbtClient) getJSON(ctx context.Context, path string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, q.base+path, nil)
	resp, err := q.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("qBittorrent HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (q *qbtClient) setFilePriority(ctx context.Context, hash string, ids []int, priority int) error {
	if len(ids) == 0 {
		return nil
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	form := url.Values{"hash": {hash}, "id": {strings.Join(parts, "|")}, "priority": {strconv.Itoa(priority)}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/torrents/filePrio", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := q.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (q *qbtClient) stop(ctx context.Context, hash string) {
	form := url.Values{"hashes": {hash}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/torrents/stop", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, err := q.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (q *qbtClient) start(ctx context.Context, hash string) {
	form := url.Values{"hashes": {hash}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/torrents/start", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, err := q.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (q *qbtClient) remove(ctx context.Context, hash string) {
	form := url.Values{"hashes": {hash}, "deleteFiles": {"false"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/torrents/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, err := q.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (q *qbtClient) waitFiles(ctx context.Context, hash string) ([]qbtFile, error) {
	for i := 0; i < 30; i++ {
		files, err := q.files(ctx, hash)
		if err == nil && len(files) > 0 {
			return files, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, errors.New("等待 qBittorrent 元数据超时")
}

func (m *Manager) runQbt(ctx context.Context, t *Task) error {
	set := m.getSettings()
	q, err := newQbt(set.QbtURL, set.QbtUser, set.QbtPass)
	if err != nil {
		return err
	}
	if err := q.login(ctx); err != nil {
		return err
	}
	hash := TorrentHash(t.M.URL)
	job, ok, err := m.st.GetJob(hash)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("磁力任务不存在")
	}
	defer func() {
		switch t.M.State {
		case "cancelled":
			q.remove(context.Background(), hash)
		default:
			q.stop(context.Background(), hash)
		}
	}()
	if err := q.addTorrent(ctx, t.M.URL, job.SaveDir); err != nil {
		return err
	}
	files, err := q.waitFiles(ctx, hash)
	if err != nil {
		return err
	}
	selected := map[int]bool{}
	for _, idx := range t.M.Files {
		selected[idx] = true
	}
	var skip []int
	for i := range files {
		if selected[i] {
			if err := q.setFilePriority(ctx, hash, []int{i}, 1); err != nil {
				return err
			}
		} else {
			skip = append(skip, i)
		}
	}
	_ = q.setFilePriority(ctx, hash, skip, 0)
	q.start(ctx, hash)
	m.st.UpdateJobState(hash, "downloading")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			info, err := q.info(ctx, hash)
			if err != nil {
				m.logf("qbt", err.Error())
				continue
			}
			m.mu.Lock()
			t.M.Total = info.Size
			t.M.Bytes = info.Completed
			t.speed = info.DlSpeed
			m.mu.Unlock()
			if fs, err := q.files(ctx, hash); err == nil {
				for _, f := range fs {
					if !selected[f.Index] {
						continue
					}
					state := "downloading"
					if f.Progress >= 1 {
						state = "done"
					}
					m.st.UpdateJobFile(hash, f.Index, state, int64(f.Progress*float64(f.Size)), "", "")
				}
			}
			if info.Progress >= 1 || (info.Size > 0 && info.Completed >= info.Size) {
				goto complete
			}
		}
	}

complete:
	if fs, err := q.files(ctx, hash); err == nil {
		for _, f := range fs {
			if !selected[f.Index] || !isVideoFile(f.Name) {
				continue
			}
			path := filepath.Join(job.SaveDir, filepath.FromSlash(f.Name))
			if _, err := os.Stat(path); err != nil {
				continue
			}
			token := fmt.Sprintf("local@%s_%d", hash, f.Index)
			if err := m.st.UpsertRows([]model.Row{{
				Token: token, Status: "done", Source: "local", Site: "local",
				Title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
				URL:   path, PageURL: filepath.Dir(path), DownloadKind: "local",
				FileName: filepath.Base(path), FilePath: path,
				FileSize: fmt.Sprintf("%d", f.Size), FileDate: time.Now().Format("2006-01-02 15:04:05"),
				FirstSeen: time.Now().Format("2006-01-02 15:04:05"),
				LastSeen:  time.Now().Format("2006-01-02 15:04:05"), Note: "auto:qbt",
			}}); err == nil {
				m.autoCover(token, path)
				if m.onRow != nil {
					m.onRow(token)
				}
				m.st.UpdateJobFile(hash, f.Index, "done", f.Size, "", token)
			}
		}
	}
	m.st.UpdateJobState(hash, "done")
	return nil
}
