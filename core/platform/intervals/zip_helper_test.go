package intervals

import (
	"compress/gzip"
	"io"
	"strings"
)

// gzipWriter 便于测试里生成 gzip 数据。
func gzipWriter(sb *strings.Builder) *gzip.Writer { return gzip.NewWriter(&sbWriter{sb}) }

type sbWriter struct{ sb *strings.Builder }

func (w *sbWriter) Write(p []byte) (int, error) { return w.sb.Write(p) }

var _ io.Writer = (*sbWriter)(nil)
