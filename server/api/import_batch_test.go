package api

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/openrunner/core"
)

// batchUpload 一次 multipart 请求上传多个文件。
func batchUpload(t *testing.T, h http.Handler, token, athleteID string, paths []string) *httptest.ResponseRecorder {
	t.Helper()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		for _, p := range paths {
			f, err := os.Open(p)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			fw, err := mw.CreateFormFile("file", filepathBase(p))
			if err != nil {
				f.Close()
				_ = pw.CloseWithError(err)
				return
			}
			if _, err := io.Copy(fw, f); err != nil {
				f.Close()
				_ = pw.CloseWithError(err)
				return
			}
			f.Close()
		}
		_ = mw.Close()
	}()
	r, err := http.NewRequest(http.MethodPost, "/api/athletes/"+athleteID+"/activities/import-batch", pr)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

const td = "../../../openrunner-core/ingest/testdata/"

func TestMultiFormatImport(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("GPX 单文件导入 → 201 且 sport=run", func(t *testing.T) {
		w := uploadFIT(t, h, token, "1", td+"simple.gpx")
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		var act struct {
			Sport     string  `json:"sport"`
			DistanceM float64 `json:"distanceM"`
			HasStream bool    `json:"hasStream"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &act)
		if act.Sport != "run" || !act.HasStream {
			t.Fatalf("期望 run+stream, 得到 %+v", act)
		}
		if act.DistanceM <= 0 {
			t.Errorf("期望 GPS 推导距离>0, 得到 %v", act.DistanceM)
		}
	})

	t.Run("TCX 单文件导入 → 201 且 sport=ride 带功率", func(t *testing.T) {
		w := uploadFIT(t, h, token, "1", td+"simple.tcx")
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		var act struct {
			Sport string `json:"sport"`
			ID    uint   `json:"id"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &act)
		if act.Sport != "ride" {
			t.Fatalf("期望 ride, 得到 %q", act.Sport)
		}
		// stream 端点应含功率
		w = do(h, http.MethodGet, "/api/athletes/1/activities/2/stream", token, "")
		stream, err := core.UnmarshalStream(w.Body.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if stream.Power[0] != 200 {
			t.Errorf("期望功率 200, 得到 %v", stream.Power[0])
		}
	})

	t.Run("未知格式 → 400", func(t *testing.T) {
		w := uploadFIT(t, h, token, "1", td+"corrupt.fit")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})
}

func TestBatchImport(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("批量：FIT+GPX+TCX 三文件 → 全部成功", func(t *testing.T) {
		w := batchUpload(t, h, token, "1", []string{
			td + "big_activity.fit", td + "simple.gpx", td + "simple.tcx",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Imported int `json:"imported"`
			Skipped  int `json:"skipped"`
			Failed   int `json:"failed"`
			Results  []struct {
				File   string `json:"file"`
				Status string `json:"status"`
				Reason string `json:"reason"`
			} `json:"results"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Imported != 3 || res.Failed != 0 {
			t.Fatalf("期望 3 成功 0 失败, 得到 %+v", res)
		}
		if len(res.Results) != 3 {
			t.Fatalf("期望 3 条结果, 得到 %d", len(res.Results))
		}
	})

	t.Run("批量重复 → 全部跳过（幂等）", func(t *testing.T) {
		w := batchUpload(t, h, token, "1", []string{td + "simple.gpx", td + "simple.tcx"})
		var res struct {
			Imported int `json:"imported"`
			Skipped  int `json:"skipped"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Imported != 0 || res.Skipped != 2 {
			t.Fatalf("期望 0 导入 2 跳过, 得到 %+v", res)
		}
	})

	t.Run("批量部分失败不影响其余（坏文件 + 好文件）", func(t *testing.T) {
		w := batchUpload(t, h, token, "1", []string{td + "corrupt.fit", td + "simple.gpx"})
		var res struct {
			Imported int `json:"imported"`
			Skipped  int `json:"skipped"`
			Failed   int `json:"failed"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		// corrupt 失败 1；simple.gpx 已导入过 → 跳过 1
		if res.Failed != 1 || res.Skipped != 1 {
			t.Fatalf("期望 1 失败 1 跳过, 得到 %+v", res)
		}
	})
}
