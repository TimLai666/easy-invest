package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPruneBackupsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	// 建 20 份備份檔（檔名時間戳遞增）+ 幾個不相關檔案。
	names := []string{
		"easy_invest_20260101_000000.dump",
		"easy_invest_20260102_000000.dump",
		"easy_invest_20260103_000000.dump",
		"easy_invest_20260104_000000.dump",
		"easy_invest_20260105_000000.dump",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 不該被輪替刪掉的其他檔案。
	for _, n := range []string{"README.txt", "easy_invest_partial.dump.partial", "other.dump"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := PruneBackups(dir, 2)
	if err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	if removed != 3 {
		t.Fatalf("刪除數 = %d, want 3", removed)
	}

	left, _ := os.ReadDir(dir)
	got := map[string]bool{}
	for _, e := range left {
		got[e.Name()] = true
	}
	// 應保留最新兩份與所有非備份檔。
	for _, want := range []string{
		"easy_invest_20260104_000000.dump",
		"easy_invest_20260105_000000.dump",
		"README.txt", "easy_invest_partial.dump.partial", "other.dump",
	} {
		if !got[want] {
			t.Fatalf("應保留 %s，實際：%v", want, got)
		}
	}
	if got["easy_invest_20260101_000000.dump"] {
		t.Fatal("最舊備份應被刪除")
	}
}

func TestPruneBackupsNoopCases(t *testing.T) {
	dir := t.TempDir()
	if n, err := PruneBackups(dir, 0); err != nil || n != 0 {
		t.Fatalf("keep=0 應不刪除，got n=%d err=%v", n, err)
	}
	if n, err := PruneBackups(filepath.Join(dir, "missing"), 14); err != nil || n != 0 {
		t.Fatalf("不存在的目錄應回 0/nil，got n=%d err=%v", n, err)
	}
}
