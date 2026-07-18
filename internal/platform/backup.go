package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 備份檔命名：easy_invest_<YYYYMMDD_HHMMSS>.dump（pg_dump custom 格式）。
// 檔名含時間戳，字典序即時間序，方便輪替與還原挑選。
const (
	backupPrefix = "easy_invest_"
	backupExt    = ".dump"
)

// Backup 以 pg_dump（custom 格式）把資料庫備份到 dir，回傳產生的檔案路徑。
// 需要執行環境有 pg_dump 可執行檔（見 Dockerfile 安裝的 postgresql client）。
//
// 先寫入 .partial 暫存檔，成功後才改名為正式檔，避免備份中斷留下半截檔
// 被輪替或還原誤認為有效備份。
func Backup(ctx context.Context, databaseURL, dir string, now time.Time) (string, error) {
	if databaseURL == "" {
		return "", fmt.Errorf("備份需要 DATABASE_URL")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("建立備份目錄失敗：%w", err)
	}
	name := backupPrefix + now.Format("20060102_150405") + backupExt
	path := filepath.Join(dir, name)
	tmp := path + ".partial"

	cmd := exec.CommandContext(ctx, "pg_dump",
		"--format=custom", "--no-owner", "--no-privileges",
		"--file", tmp, "--dbname", databaseURL)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("pg_dump 失敗：%w：%s", err, strings.TrimSpace(stderr.String()))
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("備份改名失敗：%w", err)
	}
	return path, nil
}

// PruneBackups 只保留最新的 keep 份備份，刪除更舊的，回傳實際刪除數。
// keep <= 0 時不刪除任何檔案。
func PruneBackups(dir string, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, backupPrefix) && strings.HasSuffix(name, backupExt) {
			files = append(files, name)
		}
	}
	if len(files) <= keep {
		return 0, nil
	}
	sort.Strings(files) // 檔名含時間戳，字典序即時間序
	removed := 0
	for _, name := range files[:len(files)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
