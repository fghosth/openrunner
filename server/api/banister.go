package api

import (
	"math"
	"net/http"
	"time"

	"github.com/fghosth/openrunner/core"
	"github.com/fghosth/openrunner/server/store"
	"gorm.io/gorm"
)

// banisterRefDistance 是成绩归一化的参考距离（10 km）。
// 不同距离的成绩必须用 Riegel 公式换算到同一参考距离才能比较。
const banisterRefDistance = 10000.0

// nanToNil 把 NaN 转成 JSON 里的 null —— encoding/json 拒绝编码 NaN，
// 直接塞进 map 会让整个响应报错。调整 R² 在 n ≤ p 时无定义，正好会踩到。
func nanToNil(v float64) any {
	if math.IsNaN(v) {
		return nil
	}
	return v
}

// banisterRecencyLambdaDays 近期权重尺度：w = exp(−age/λ)。λ→∞ 退化成不加权。
// 90 天是「一个训练周期」的量级：上周的成绩权重≈1，三个月前≈0.37，半年前≈0.14。
// 这是 Banister 原式之外的扩展，理由与量化见 docs/agents/golden-checks.md。
const banisterRecencyLambdaDays = 90.0

// formBiasWindow / formBiasMinFrac 系统性偏差的判定口径：最近 3 条成绩全部
// 同号偏离，且平均相对偏差 ≥ 2%，才认为模型没跟上当前状态。
const (
	formBiasWindow  = 3
	formBiasMinFrac = 0.02
)

// handleBanister 用比赛成绩 + 日训练负荷拟合 Banister 表现模型，并给出未来预测。
//
// GET /api/athletes/{athleteId}/banister?metric=trimp|rtss|power
func handleBanister(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		if ok, err := store.AthleteExists(db, uint(athleteID)); err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		} else if !ok {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}

		// 白名单（列名直接拼进 SQL）：trimp / rtss / power（= PowerLoad）。
		metric := r.URL.Query().Get("metric")
		switch metric {
		case "rtss", "power":
		default:
			metric = "trimp"
		}
		col := metric
		if col == "power" {
			col = "power_load"
		}
		rows, err := loadDailyLoads(db, uint(athleteID), col)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "聚合训练负荷失败")
			return
		}
		loads := map[string]float64{}
		for _, row := range rows {
			if row.IsPlanned {
				continue
			}
			loads[timeToDateString(row.StartTime)] += row.Value
		}

		results, err := store.ListRaceResults(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询比赛成绩失败")
			return
		}
		if len(results) < 3 {
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted": false,
				"reason": "需要至少 3 次比赛/计时成绩才能拟合（当前 " +
					itoa(int64(len(results))) + " 次）—— 去设置页录入，自己跑的 5K/10K 全力也算",
				"reasonCode":       "needThreeResults",
				"reasonCodeParams": map[string]any{"n": len(results)},
				"count":            len(results),
				"refDistanceM":     banisterRefDistance,
			})
			return
		}

		races := make([]core.RacePoint, 0, len(results))
		for _, rr := range results {
			perf := core.RacePerformance(rr.DistanceM, float64(rr.TimeSecs), banisterRefDistance)
			if perf > 0 {
				races = append(races, core.RacePoint{Date: rr.Date, Performance: perf})
			}
		}
		// 成绩够多时连 τ 一起拟合（5 参数）；不足 BanisterMinRacesForTauFit 条时
		// 内部退回固定 τ=45/15，调用方不用分支
		model := core.FitBanisterFitTau(loads, races, banisterRecencyLambdaDays)
		// 拟合完之后先量一次「最近几条是不是被系统性算慢了」，
		// 成立时下面会额外给一组锚在最近实际成绩上的预测（见 projectionsAnchored）
		bias := core.DetectFormBias(model, races, banisterRefDistance, formBiasWindow, formBiasMinFrac)
		if model.RMSE < 0 {
			writeJSON(w, http.StatusOK, map[string]any{
				"fitted":     false,
				"reason":     "有效成绩不足（日期或数值异常），无法拟合",
				"reasonCode": "invalidResults",
				"count":      len(races),
			})
			return
		}

		// 历史点：实际 vs 模型预测
		type point struct {
			Date      string  `json:"date"`
			Actual    float64 `json:"actual"`    // 等价 10K 速度 m/s
			Predicted float64 `json:"predicted"` // 模型给出
			PaceSecs  float64 `json:"paceSecsPerKm"`
		}
		points := make([]point, 0, len(races))
		for _, rp := range races {
			points = append(points, point{
				Date:      rp.Date,
				Actual:    rp.Performance,
				Predicted: model.Predict(rp.Date),
				PaceSecs:  1000 / rp.Performance,
			})
		}

		// 未来 30 天预测（假设不训练，只看当前疲劳消退后的走势）
		type fc struct {
			Date      string  `json:"date"`
			Predicted float64 `json:"predicted"`
			PaceSecs  float64 `json:"paceSecsPerKm"`
		}
		forecast := make([]fc, 0, 30)
		today := time.Now().Local()
		for i := 1; i <= 30; i++ {
			d := today.AddDate(0, 0, i).Format("2006-01-02")
			p := model.Predict(d)
			if p <= 0 {
				continue
			}
			forecast = append(forecast, fc{Date: d, Predicted: p, PaceSecs: 1000 / p})
		}

		// ---- 距离外推：5K / 10K / 半马 / 全马 ----
		// 1) 个人 Riegel 指数 α：用用户自己的多距离成绩 log-log 回归
		//    （比固定 1.06 准；业余跑者 α 常在 1.08–1.15）
		// 2) 基准 = Banister 给出的「今天」等价 10K 表现
		// 3) t_d = t_10k · (d/10km)^α
		// FitRiegelIndex 需要「距离 + 用时」原始点（不是 Banister 的 RacePoint）
		efforts := make([]core.EffortPoint, 0, len(results))
		for _, rr := range results {
			efforts = append(efforts, core.EffortPoint{
				Seconds: float64(rr.TimeSecs), DistanceM: rr.DistanceM,
			})
		}
		rieger := core.FitRiegelIndex(efforts)
		alpha := core.DefaultRiegelAlpha
		alphaSource := "default"
		if rieger.OK {
			alpha = rieger.Alpha
			alphaSource = "personal"
		}

		// 用户已知的最长成绩距离（判断外推是否离数据太远）
		maxKnown := 0.0
		for _, rr := range results {
			if rr.DistanceM > maxKnown {
				maxKnown = rr.DistanceM
			}
		}

		targets := []struct {
			Label     string
			LabelKey  string
			DistanceM float64
		}{
			{"5K", "race.5k", 5000},
			{"10K", "race.10k", 10000},
			{"半马", "race.half", 21097.5},
			{"全马", "race.marathon", 42195},
		}

		// 基准 = Banister 拟合出的「今天」等价 10K 表现
		var projections []core.RaceProjection
		// projectionsAnchored 只在检测到系统性偏差时给出：基准换成最近几条**实际**
		// 成绩的平均等价表现，即「用直接证据，不用没跟上的模型」。
		var projectionsAnchored []core.RaceProjection
		projectionBasis := "model"
		basePerf := model.Predict(time.Now().Local().Format("2006-01-02"))
		if basePerf > 0 {
			baseTime10k := banisterRefDistance / basePerf // 等价 10K 用时（秒）
			projections = core.ProjectRaceTimes(
				banisterRefDistance, baseTime10k, targets, alpha, maxKnown,
			)
		}
		if bias.AnchorPerf > 0 {
			anchored := core.ProjectRaceTimes(
				banisterRefDistance, banisterRefDistance/bias.AnchorPerf, targets, alpha, maxKnown,
			)
			// 模型明显偏慢时，锚定值才是该给用户看的那一组；偏快时保留模型值为主
			projectionsAnchored = anchored
			if bias.Direction == "modelSlow" {
				projectionBasis = "anchored"
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"fitted":      true,
			"projections": projections,
			"rieger": map[string]any{
				"alpha":  alpha,
				"source": alphaSource, // personal | default
				"n":      rieger.N,
				"ok":     rieger.OK,
				"r2":     rieger.R2,
			},
			"maxKnownDistanceM": maxKnown,
			"p0":                model.P0,
			"k1":                model.K1,
			"k2":                model.K2,
			"tau1":              model.Tau1,
			"tau2":              model.Tau2,
			"r2":                model.R2,
			"overfit":           model.Overfit,
			"lambda":            model.Lambda,
			"bias":              bias,
			"projectionBasis":   projectionBasis,
			"tauFitted":         model.TauFitted,
			"tauAtBoundary":     model.TauAtBoundary,
			// NaN 不是合法 JSON，encoding/json 会直接报错，必须转成 null
			"r2Adj":               nanToNil(model.R2Adj),
			"params":              model.Params,
			"projectionsAnchored": projectionsAnchored,
			"rmse":                model.RMSE,
			"n":                   model.N,
			"refDistanceM":        banisterRefDistance,
			"points":              points,
			"forecast":            forecast,
		})
	}
}
