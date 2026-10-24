package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/openrunner/core"
)

func unmarshalStreamForTest(b []byte) (core.Stream, error) {
	return core.UnmarshalStream(b)
}

func setupWithToken(t *testing.T) (http.Handler, string) {
	t.Helper()
	h, _ := newTestServer(t)
	token := setupAdmin(t, h)
	return h, token
}

// createAthlete 建一个档案并返回 id。
func createAthlete(t *testing.T, h http.Handler, token string) uint {
	t.Helper()
	w := do(h, http.MethodPost, "/api/athletes", token, `{"name":"测试档案"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建档案失败: %d", w.Code)
	}
	var m struct {
		ID uint `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return m.ID
}

// uploadFIT 用 multipart 上传 FIT 文件。
func uploadFIT(t *testing.T, h http.Handler, token, athleteID, fixturePath string) *httptest.ResponseRecorder {
	t.Helper()
	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("打开 fixture 失败: %v", err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := newMultipartWriter(pw)
	go func() {
		defer pw.Close()
		if err := mw.WriteFile("file", fixturePath, f); err != nil {
			_ = pw.CloseWithError(err)
		}
		if err := mw.Close(); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()

	r, err := http.NewRequest(http.MethodPost, "/api/athletes/"+athleteID+"/activities/import", pr)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestActivityImport(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("上传合法 FIT → 201", func(t *testing.T) {
		w := uploadFIT(t, h, token, "1", "../../../openrunner-core/ingest/testdata/big_activity.fit")
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		var act struct {
			ID        uint    `json:"id"`
			Sport     string  `json:"sport"`
			DistanceM float64 `json:"distanceM"`
			DurationS int     `json:"durationSecs"`
			HasStream bool    `json:"hasStream"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &act)
		if act.Sport != "ride" {
			t.Errorf("期望 ride, 得到 %q", act.Sport)
		}
		if act.DistanceM <= 0 {
			t.Errorf("期望正距离, 得到 %v", act.DistanceM)
		}
		if act.DurationS <= 0 {
			t.Errorf("期望正时长, 得到 %v", act.DurationS)
		}
		if !act.HasStream {
			t.Error("期望 hasStream=true")
		}
		if act.ID == 0 {
			t.Error("期望返回 id")
		}
	})

	t.Run("列表包含新导入活动", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/activities", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		var list []map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 {
			t.Fatalf("期望 1 条, 得到 %d", len(list))
		}
	})

	t.Run("详情返回 summary", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/activities/1", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		var act struct {
			HasStream bool `json:"hasStream"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &act)
		if !act.HasStream {
			t.Error("期望 hasStream=true")
		}
	})

	t.Run("stream 端点返回可还原 BLOB", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/activities/1/stream", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		stream, err := unmarshalStreamForTest(w.Body.Bytes())
		if err != nil {
			t.Fatalf("还原失败: %v", err)
		}
		if stream.Seconds() < 30000 {
			t.Errorf("期望长 stream, 得到 %d", stream.Seconds())
		}
		if stream.SpeedKph[0] == 0 {
			t.Error("期望首点速度非零")
		}
	})

	t.Run("损坏文件 → 400 可读错误", func(t *testing.T) {
		w := uploadFIT(t, h, token, "1", "../../../openrunner-core/ingest/testdata/corrupt.fit")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
		if w.Body.String() == "" {
			t.Fatal("期望错误信息")
		}
	})

	t.Run("重复导入同一文件 → 409（幂等防重）", func(t *testing.T) {
		w := uploadFIT(t, h, token, "1", "../../../openrunner-core/ingest/testdata/big_activity.fit")
		if w.Code != http.StatusConflict {
			t.Fatalf("期望 409, 得到 %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("不存在的 athlete → 404", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/999/activities", token, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望 404, 得到 %d", w.Code)
		}
	})
}
