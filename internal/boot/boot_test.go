package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p
}

func TestStartAndSingleInstance(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"kind":"reelkeep-project"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := ResolveProject(root)
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	logf := func(format string, args ...any) { t.Logf(format, args...) }

	inst, err := Start(context.Background(), Options{
		Paths: paths, Port: port, Version: "test", NoBrowser: true, Logf: logf,
	})
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if inst.Existing {
		t.Fatal("首次启动不应是 Existing")
	}
	defer inst.Close()

	var probe struct {
		OK  bool `json:"ok"`
		API int  `json:"api"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/state", inst.Port))
		if err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&probe)
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("服务没起来: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !probe.OK || probe.API != 4 {
		t.Fatalf("接口不对: %+v", probe)
	}

	// 同端口再启动：应识别为已有实例，且不重复打开数据库
	second, err := Start(context.Background(), Options{
		Paths: paths, Port: port, Version: "test", NoBrowser: true, Logf: logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Existing {
		t.Fatal("第二次启动应识别为已有实例")
	}
	second.Close()

	// 关闭后端口应释放，可以重新启动
	inst.Close()
	time.Sleep(200 * time.Millisecond)
	third, err := Start(context.Background(), Options{
		Paths: paths, Port: port, Version: "test", NoBrowser: true, Logf: logf,
	})
	if err != nil {
		t.Fatalf("关闭后重新启动失败: %v", err)
	}
	if third.Existing {
		t.Fatal("关闭后不应再是 Existing")
	}
	third.Close()
}
