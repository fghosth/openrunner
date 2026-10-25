package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// handleAnnualPoster 年度 SVG 海报（票 14）：可直接下载/分享。
func handleAnnualPoster(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		athlete, err := store.GetAthlete(db, uint(athleteID))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		year := time.Now().Local().Year()
		if q := r.URL.Query().Get("year"); q != "" {
			v, err := strconv.Atoi(q)
			if err != nil || v < 1900 || v > 2200 {
				writeError(w, http.StatusBadRequest, "非法年份")
				return
			}
			year = v
		}
		days, err := loadDayActivities(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "聚合失败")
			return
		}
		stat := core.AnnualStats(days, year)

		accent := r.URL.Query().Get("accent") // 可选自定义强调色
		svg := core.RenderAnnualPoster(stat, core.PosterOptions{
			AthleteName: athlete.Name,
			Accent:      accent,
		})
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`inline; filename="openrunner-%d-%d.svg"`, athlete.ID, year))
		_, _ = w.Write([]byte(svg))
	}
}
