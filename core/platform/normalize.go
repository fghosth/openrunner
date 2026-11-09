package platform

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

// NormalizeOriginal 把下载到的原始文件统一成 ingest 能解析的明文 + 扩展名。
//
// 多数平台直接给 FIT/TCX/GPX；但有些（Intervals.icu）会返回 .fit.gz，
// 有些给 ZIP 包（本项目已在 garmin 客户端里单独处理过 ZIP）。
// 这里只处理最常见的 gzip 与扩展名推断，保持各平台实现不重复写这段。
func NormalizeOriginal(raw []byte) ([]byte, string, error) {
	// gzip 魔数
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, "", fmt.Errorf("解压 gzip 失败: %w", err)
		}
		defer zr.Close()
		plain, err := io.ReadAll(io.LimitReader(zr, 64<<20))
		if err != nil {
			return nil, "", fmt.Errorf("解压 gzip 失败: %w", err)
		}
		raw = plain
	}
	ext := detectExt(raw)
	if ext == "" {
		return nil, "", fmt.Errorf("无法识别的运动文件格式（既非 FIT/TCX/GPX）")
	}
	return raw, ext, nil
}

// detectExt 按魔数/内容嗅探扩展名。
func detectExt(b []byte) string {
	if len(b) > 12 && string(b[8:12]) == ".FIT" {
		return ".fit"
	}
	head := string(b)
	if i := strings.Index(head, "<?xml"); i >= 0 {
		head = head[i:]
	}
	lower := strings.ToLower(head)
	if strings.Contains(lower, "<trainingcenterdatabase") {
		return ".tcx"
	}
	if strings.Contains(lower, "<gpx") {
		return ".gpx"
	}
	return ""
}
