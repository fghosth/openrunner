package coach

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 金标准对照的 Go 侧：读 OPENRUNNER_COACH_CASES 目录下所有 *.json
// （check_week.py 的 simple 输入形状），把逐规则判定写成 JSON。
// 对照逻辑在 openrunner/scripts/coach-check.py。
func TestCoachDumpWeekCheck(t *testing.T) {
	dir := os.Getenv("OPENRUNNER_COACH_CASES")
	if dir == "" {
		t.Skip("未设置 OPENRUNNER_COACH_CASES，跳过对照 dump")
	}
	out := os.Getenv("OPENRUNNER_COACH_OUT")
	if out == "" {
		out = "/tmp/coach_go_verdicts.json"
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("用例目录无 JSON 文件: %v", err)
	}
	result := map[string][]Verdict{}
	for _, f := range files {
		in, err := loadFileInput(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		vs, err := CheckWeek(in)
		if err != nil {
			// 空计划等错误形态也记录，供对照侧核对「两边都拒绝」
			result[filepath.Base(f)] = []Verdict{{Level: "ERROR", Rule: "input", Detail: err.Error()}}
			continue
		}
		result[filepath.Base(f)] = vs
	}
	b, err := json.MarshalIndent(result, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %s：%d 个用例", out, len(result))
}
