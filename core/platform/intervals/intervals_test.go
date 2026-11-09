package intervals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fghosth/openrunner/core/platform"
)

func TestListAndDownload(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch {
		case strings.HasSuffix(r.URL.Path, "/activities"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "i100", "start_date_local": "2026-05-01T07:00:00", "type": "Run", "name": "Morning"},
				{"id": "i101", "start_date_local": "2026-05-02T18:00:00", "type": "Ride", "name": "Evening"},
			})
		case strings.HasSuffix(r.URL.Path, "/file"):
			// 返回 gzip 的 TCX
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(gzipBytes([]byte(tcxSample)))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	platform.SetBaseURL(name, srv.URL)
	defer platform.SetBaseURL(name, "")

	p := Provider{}
	cred := platform.Credential{FieldAthleteID: "i1", FieldAPIKey: "k1"}

	list, err := p.ListActivities(context.Background(), cred, time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("拉列表失败: %v", err)
	}
	if len(list) != 2 || list[0].Sport != platform.SportRun || list[1].Sport != platform.SportRide {
		t.Fatalf("列表映射不符: %+v", list)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("应使用 Basic 认证, 得到 %q", gotAuth)
	}

	data, fn, err := p.Download(context.Background(), cred, list[0])
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if !strings.Contains(string(data), "TrainingCenterDatabase") {
		t.Fatalf("应解压出 TCX 明文, 得到前 40 字节: %q", string(data[:min(40, len(data))]))
	}
	if !strings.HasSuffix(fn, ".tcx") {
		t.Fatalf("文件名应以 .tcx 结尾, 得到 %q", fn)
	}

	t.Run("401 → AuthError", func(t *testing.T) {
		badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
		}))
		defer badSrv.Close()
		platform.SetBaseURL(name, badSrv.URL)
		defer platform.SetBaseURL(name, "")
		_, err := p.ListActivities(context.Background(), cred, time.Now().AddDate(0, 0, -7))
		if !platform.IsAuthError(err) {
			t.Fatalf("期望 AuthError, 得到 %v", err)
		}
	})

	t.Run("缺字段被拒", func(t *testing.T) {
		if err := p.Validate(platform.Credential{FieldAthleteID: "i1"}); err == nil {
			t.Fatal("缺 apiKey 应报错")
		}
	})
}

const tcxSample = `<?xml version="1.0"?><TrainingCenterDatabase><Activities/></TrainingCenterDatabase>`

func gzipBytes(b []byte) []byte {
	var buf strings.Builder
	zw := gzipWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return []byte(buf.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
