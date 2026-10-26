package platform

import (
	"context"
	"io"
	"net/http"
	"time"
)

// defaultClient 是各平台共用的 HTTP 客户端。
// 单独定义（而不是用 http.DefaultClient）以便统一超时与便于测试替换。
var defaultClient = &http.Client{Timeout: 60 * time.Second}

// HTTPGet 发一个 GET 请求并读取响应体（上限 64MB，防止异常大文件把内存打爆）。
//
// 各平台实现共用的最小 HTTP 辅助 —— 不引入任何 HTTP 框架。
func HTTPGet(ctx context.Context, url string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// HTTPPostForm 发一个 application/x-www-form-urlencoded 的 POST（OAuth 换 token 用）。
func HTTPPostForm(ctx context.Context, url string, form map[string]string, headers map[string]string) ([]byte, int, error) {
	vals := make(map[string][]string, len(form))
	for k, v := range form {
		vals[k] = []string{v}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, stringsReader(vals))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// HTTPPostJSON 发 application/json 的 POST（部分平台登录用）。
func HTTPPostJSON(ctx context.Context, url string, payload []byte, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytesReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
