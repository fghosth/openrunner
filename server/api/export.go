package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// exportSchemaVersion 导出格式版本（结构变更时递增，便于导入端兼容判断）。
const exportSchemaVersion = 1

// exportActivity 导出用活动形态：summary + metrics + stream（base64）。
type exportActivity struct {
	ID             uint        `json:"id"`
	Sport          string      `json:"sport"`
	StartTime      int64       `json:"startTime"`
	DurationSecs   int         `json:"durationSecs"`
	TimerSecs      int         `json:"timerSecs"`
	DistanceM      float64     `json:"distanceM"`
	ElevationGainM float64     `json:"elevationGainM"`
	IsPlanned      bool        `json:"isPlanned"`
	HasStream      bool        `json:"hasStream"`
	AvgHr          float64     `json:"avgHr"`
	Source         string      `json:"source"`
	Metrics        *metricsDTO `json:"metrics"`
	Stream         string      `json:"stream,omitempty"` // base64(JSON core.Stream)
}

// exportHeader 导出头部：版本 + 档案 + 全部配置。
// 用结构体（非 map）保证字段顺序稳定，导出文件可 diff。
type exportHeader struct {
	SchemaVersion int                   `json:"schemaVersion"`
	ExportedAt    string                `json:"exportedAt"`
	Athlete       exportAthlete         `json:"athlete"`
	HrZones       []store.HrZoneModel   `json:"hrZones"`
	PaceZones     []store.PaceZoneModel `json:"paceZones"`
	CsZones       []store.CsZoneModel   `json:"csZones"`
	Seasons       []store.SeasonModel   `json:"seasons"`
}

type exportAthlete struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Sex  string `json:"sex"`
}

// handleExport 流式导出档案全量数据（票 12）：活动（含 stream/metrics）+ 全部配置。
// 活动逐条编码写出，避免大档案一次性驻留内存。
//
// 失败语义（Round 8）：流式输出一旦开始就无法回滚，中途出错时直接关闭连接不再写尾部——
// 客户端会得到"不完整 JSON"从而判定失败，绝不返回"合法但缺数据"的归档。
func handleExport(db *gorm.DB) http.HandlerFunc {
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

		hrZones, err1 := store.ListHrZones(db, uint(athleteID))
		paceZones, err2 := store.ListPaceZones(db, uint(athleteID))
		csZones, err3 := store.ListCsZones(db, uint(athleteID))
		seasons, err4 := store.ListSeasons(db, uint(athleteID))
		for _, e := range []error{err1, err2, err3, err4} {
			if e != nil {
				writeError(w, http.StatusInternalServerError, "查询配置失败")
				return
			}
		}
		acts, err := store.ListActivities(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询活动失败")
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="openrunner-athlete-%d-%s.json"`,
				athlete.ID, time.Now().Format("20060102")))

		header := exportHeader{
			SchemaVersion: exportSchemaVersion,
			ExportedAt:    time.Now().UTC().Format(time.RFC3339),
			Athlete:       exportAthlete{ID: athlete.ID, Name: athlete.Name, Sex: athlete.Sex},
			HrZones:       nonNilSlice(hrZones),
			PaceZones:     nonNilSlice(paceZones),
			CsZones:       nonNilSlice(csZones),
			Seasons:       nonNilSlice(seasons),
		}
		hb, err := json.Marshal(header)
		if err != nil {
			return
		}
		// 去掉 header 的结尾 '}'，续接 activities 数组并打开：
		// {"schemaVersion":1,...,"seasons":[]} → {"schemaVersion":1,...,"seasons":[],"activities":[
		if _, err := w.Write([]byte(string(hb[:len(hb)-1]) + `,"activities":[`)); err != nil {
			return
		}

		enc := json.NewEncoder(w)
		for i := range acts {
			a := &acts[i]
			out := exportActivity{
				ID: a.ID, Sport: a.Sport, StartTime: a.StartTime,
				DurationSecs: a.DurationSecs, TimerSecs: a.TimerSecs,
				DistanceM: a.DistanceM, ElevationGainM: a.ElevationGainM,
				IsPlanned: a.IsPlanned, HasStream: a.HasStream,
				AvgHr: a.AvgHr, Source: a.Source,
			}
			m, merr := getMetricsDTO(db, a.ID)
			if merr != nil {
				return // 中途失败：连接直接关闭，客户端据"JSON 不完整"识别失败（见 handleExport 注释）
			}
			out.Metrics = m
			if a.HasStream {
				sm, serr := store.GetStream(db, a.ID)
				if serr != nil {
					return
				}
				out.Stream = base64.StdEncoding.EncodeToString(sm.Data)
			}
			if i > 0 {
				if _, err := w.Write([]byte(",")); err != nil {
					return
				}
			}
			if err := enc.Encode(out); err != nil {
				return // 客户端断开等：静默结束
			}
		}
		_, _ = w.Write([]byte(`],"streamFormat":"base64(core.Stream json)"}` + "\n"))
	}
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
