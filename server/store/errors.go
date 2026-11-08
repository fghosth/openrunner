package store

import "errors"

// ErrZoneNotFound 档案下不存在该区间记录（hr/pace/cs 删除共用）。
var ErrZoneNotFound = errors.New("zone not found")
