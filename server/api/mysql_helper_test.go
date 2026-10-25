package api

import (
	"net/http"
	"testing"

	"github.com/openrunner/server/store"
)

func newTestServerMySQL(t *testing.T, dsn string) (http.Handler, string, func()) {
	t.Helper()
	db, err := store.OpenDSN(dsn)
	if err != nil {
		t.Fatalf("MySQL 连接失败: %v", err)
	}
	// 清空业务表（保留 secrets：JWT 密钥在 Open 时生成，删了会让 token 签发失败）
	for _, tbl := range []string{"activity_metrics", "activity_streams", "activities", "cs_zones",
		"seasons", "pace_zones", "hr_zones", "athletes", "users"} {
		if err := db.Gorm().Exec("DELETE FROM " + tbl).Error; err != nil {
			t.Fatalf("清表 %s 失败: %v", tbl, err)
		}
	}
	h := NewMux(db.Gorm(), "test", nil)
	token := setupAdmin(t, h)
	return h, token, func() { _ = db.Close() }
}
