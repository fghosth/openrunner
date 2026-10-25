package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/openrunner/core"
	"github.com/openrunner/core/ingest"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// parseActivityFile 按内容嗅探格式并解析（FIT 二进制 / GPX / TCX XML）。
// 嗅探策略：以 '<' 开头视为 XML（再看根元素区分 GPX/TCX），否则按 FIT。
func parseActivityFile(content []byte) (core.Activity, core.Stream, string, error) {
	trimmed := bytes.TrimLeft(content, " \t\r\n\xef\xbb\xbf")
	if len(trimmed) > 0 && trimmed[0] == '<' {
		head := string(trimmed)
		if len(head) > 4096 {
			head = head[:4096]
		}
		switch {
		case strings.Contains(head, "TrainingCenterDatabase"):
			a, s, err := ingest.ParseTCX(bytes.NewReader(content))
			return a, s, "import_tcx", err
		case strings.Contains(head, "<gpx"):
			a, s, err := ingest.ParseGPX(bytes.NewReader(content))
			return a, s, "import_gpx", err
		default:
			return core.Activity{}, core.Stream{}, "", fmt.Errorf("无法识别的 XML 活动格式")
		}
	}
	a, s, err := ingest.ParseFIT(bytes.NewReader(content))
	return a, s, "import_fit", err
}

// importOutcome 单文件导入结果。
type importOutcome struct {
	File       string `json:"file"`
	Status     string `json:"status"` // imported / skipped / failed
	Reason     string `json:"reason,omitempty"`
	ActivityID uint   `json:"activityId,omitempty"`
}

// importOne 处理单文件：去重 → 解析 → 落库 → 算指标。返回 outcome。
func importOne(db *gorm.DB, athleteID uint, filename string, content []byte) importOutcome {
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])

	dup, err := store.FindActivityByHash(db, athleteID, hash)
	if err != nil {
		return importOutcome{File: filename, Status: "failed", Reason: "查重失败"}
	}
	if dup != nil {
		return importOutcome{File: filename, Status: "skipped", Reason: "该文件已导入", ActivityID: dup.ID}
	}

	activity, stream, source, err := parseActivityFile(content)
	if err != nil {
		return importOutcome{File: filename, Status: "failed", Reason: err.Error()}
	}
	// 只收录跑步与骑行：游泳/力量/球类等既无速度或功率语义，也会污染 PMC、
	// 个人最佳、配速统计，解析出来就丢掉（状态记为 skipped 以便用户看到原因）。
	if !core.IsCollectedSport(activity.Sport) {
		return importOutcome{
			File: filename, Status: "skipped",
			Reason: "只收录跑步与骑行（该文件为 " + string(activity.Sport) + "）",
		}
	}
	id, err := store.CreateActivityWithStream(db, athleteID, activity, stream, source, hash)
	if err != nil {
		return importOutcome{File: filename, Status: "failed", Reason: "保存失败"}
	}
	m, err := store.GetActivity(db, athleteID, id)
	if err != nil {
		return importOutcome{File: filename, Status: "failed", Reason: "查询失败"}
	}
	if athlete, aerr := store.GetAthlete(db, athleteID); aerr == nil {
		_ = computeMetrics(db, athlete, m, &stream) // 尽力而为
	}
	return importOutcome{File: filename, Status: "imported", ActivityID: id}
}

// handleActivityImportBatch 批量导入（多次 multipart file 字段）：
// 逐文件独立处理，单文件失败不影响其余；返回 imported/skipped/failed 汇总与逐文件结果。
func handleActivityImportBatch(db *gorm.DB) http.HandlerFunc {
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

		// 批量上限：单文件 64MB，总量按文件数放大（最多 64 个文件）
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes*64)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "multipart 解析失败")
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		var headers []*multipart.FileHeader
		if r.MultipartForm != nil && r.MultipartForm.File != nil {
			headers = r.MultipartForm.File["file"]
		}
		if len(headers) == 0 {
			writeError(w, http.StatusBadRequest, "缺少 file 字段")
			return
		}

		results := make([]importOutcome, 0, len(headers))
		counts := map[string]int{}
		for _, fh := range headers {
			out := importOneFileHeader(db, uint(athleteID), fh)
			results = append(results, out)
			counts[out.Status]++
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"imported": counts["imported"],
			"skipped":  counts["skipped"],
			"failed":   counts["failed"],
			"results":  results,
		})
	}
}

func importOneFileHeader(db *gorm.DB, athleteID uint, fh *multipart.FileHeader) importOutcome {
	f, err := fh.Open()
	if err != nil {
		return importOutcome{File: fh.Filename, Status: "failed", Reason: "无法打开上传文件"}
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, maxUploadBytes))
	if err != nil {
		return importOutcome{File: fh.Filename, Status: "failed", Reason: "读取失败"}
	}
	return importOne(db, athleteID, fh.Filename, content)
}
