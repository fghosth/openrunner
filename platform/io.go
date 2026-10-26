package platform

import (
	"bytes"
	"io"
	"net/url"
	"strings"
)

// 小工具：避免在多个实现里重复 import bytes/strings/url。

func stringsReader(vals map[string][]string) io.Reader {
	return strings.NewReader(url.Values(vals).Encode())
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
