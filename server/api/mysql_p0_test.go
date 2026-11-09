package api

import (
	"net/http"
	"os"
	"testing"
)

// MySQL 下的 P0 回归：同一档案多个手工活动（content_hash NULL 语义）
func TestMySQLManualActivities(t *testing.T) {
	dsn := os.Getenv("OPENRUNNER_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("无 MySQL DSN")
	}
	h, token, closeFn := newTestServerMySQL(t, dsn)
	defer closeFn()
	_ = createAthlete(t, h, token)
	for i, d := range []string{"2026-05-01", "2026-05-02", "2026-05-03"} {
		w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
			`{"date":"`+d+`","sport":"run","durationSecs":3600,"distanceM":10000,"isPlanned":true}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("第 %d 个手工活动失败: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	// 大 stream（>64KB）验证 longblob
	if w := uploadFIT(t, h, token, "1", "../../core/ingest/testdata/big_activity.fit"); w.Code != http.StatusCreated {
		t.Fatalf("大 FIT 导入失败（blob 未改 longblob？）: %d %s", w.Code, w.Body.String())
	}
}
