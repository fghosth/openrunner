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

		// 可选自定义强调色：只接受 #RRGGBB / #RGB，直接拼进 SVG 属性必须白名单校验（防注入）
		accent := r.URL.Query().Get("accent")
		if accent != "" && !isHexColor(accent) {
			writeError(w, http.StatusBadRequest, "accent 需为 #RRGGBB 或 #RGB")
			return
		}
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

// isHexColor 校验 #RGB / #RRGGBB（accent 直接注入 SVG 属性，必须白名单）。
func isHexColor(s string) bool {
	if len(s) != 4 && len(s) != 7 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
