package api

import (
	"mime/multipart"
	"io"
)

// multipartWriter 封装 multipart 表单写入（测试与后续批量导入共用）。
type multipartWriter struct{ *multipart.Writer }

func newMultipartWriter(w io.Writer) *multipartWriter {
	return &multipartWriter{multipart.NewWriter(w)}
}

// WriteFile 写入一个文件表单字段。
func (m *multipartWriter) WriteFile(field, filename string, r io.Reader) error {
	fw, err := m.CreateFormFile(field, filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, r)
	return err
}
