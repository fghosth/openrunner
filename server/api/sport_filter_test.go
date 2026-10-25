package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// swimTCX 是一个游泳活动（应被拒绝收录）。
const swimTCX = `<?xml version="1.0" encoding="UTF-8"?>
<TrainingCenterDatabase xmlns="http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2">
  <Activities><Activity Sport="Swimming">
    <Id>2026-10-20T10:00:00Z</Id>
    <Lap StartTime="2026-10-20T10:00:00Z">
      <TotalTimeSeconds>600</TotalTimeSeconds><DistanceMeters>800</DistanceMeters>
      <Track>
        <Trackpoint><Time>2026-10-20T10:00:00Z</Time><Position><LatitudeDegrees>31.40</LatitudeDegrees><LongitudeDegrees>121.40</LongitudeDegrees></Position></Trackpoint>
        <Trackpoint><Time>2026-10-20T10:10:00Z</Time><Position><LatitudeDegrees>31.41</LatitudeDegrees><LongitudeDegrees>121.41</LongitudeDegrees></Position></Trackpoint>
      </Track>
    </Lap>
  </Activity></Activities>
</TrainingCenterDatabase>`

func writeSwimFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "swim.tcx")
	if err := os.WriteFile(p, []byte(swimTCX), 0o600); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}
	return p
}

// TestOnlyRunAndRideCollected 验证「只收录跑步与骑行」在所有导入路径生效。
func TestOnlyRunAndRideCollected(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)
	fixture := writeSwimFixture(t)

	t.Run("单文件导入游泳被拒绝且不落库", func(t *testing.T) {
		w := uploadFIT(t, h, token, "1", fixture)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("期望 422, 得到 %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "只收录跑步与骑行") {
			t.Fatalf("错误信息应说明收录范围, 得到 %s", w.Body.String())
		}
		list := do(h, http.MethodGet, "/api/athletes/1/activities", token, "")
		if strings.Contains(list.Body.String(), `"sport":"swim"`) {
			t.Fatal("游泳活动不应被保存")
		}
	})

	t.Run("批量导入时游泳被 skipped 并说明原因", func(t *testing.T) {
		w := batchUpload(t, h, token, "1", []string{fixture})
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"status":"skipped"`) {
			t.Fatalf("游泳应被 skipped, 得到 %s", w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "只收录跑步与骑行") {
			t.Fatalf("应说明原因, 得到 %s", w.Body.String())
		}
	})
}
