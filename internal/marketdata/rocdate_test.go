package marketdata

import (
	"testing"
	"time"
)

func TestParseROCCompactDate(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"1150611", "2026-06-11", false},
		{"1150101", "2026-01-01", false},
		{"0990104", "2010-01-04", false},
		{" 1150611 ", "2026-06-11", false}, // 容忍前後空白
		{"115061", "", true},               // 長度不足
		{"11506111", "", true},             // 長度過長
		{"115-611", "", true},              // 非數字
		{"1150230", "", true},              // 2 月 30 日不存在
		{"1151301", "", true},              // 13 月
		{"", "", true},
	}
	for _, tc := range cases {
		got, err := ParseROCCompactDate(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseROCCompactDate(%q) 應該失敗，卻得到 %v", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseROCCompactDate(%q) 失敗: %v", tc.input, err)
			continue
		}
		if got.Format("2006-01-02") != tc.want {
			t.Errorf("ParseROCCompactDate(%q) = %s，預期 %s", tc.input, got.Format("2006-01-02"), tc.want)
		}
	}
}

func TestParseROCSlashDate(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"115/06/01", "2026-06-01", false},
		{"109/06/02", "2020-06-02", false},
		{"99/01/04", "2010-01-04", false},
		{"115/6/1", "2026-06-01", false}, // 不補零也要能解析
		{"2026/06/01", "", true},         // 西元年超出民國年合理範圍
		{"115-06-01", "", true},
		{"115/06", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		got, err := ParseROCSlashDate(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseROCSlashDate(%q) 應該失敗，卻得到 %v", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseROCSlashDate(%q) 失敗: %v", tc.input, err)
			continue
		}
		if got.Format("2006-01-02") != tc.want {
			t.Errorf("ParseROCSlashDate(%q) = %s，預期 %s", tc.input, got.Format("2006-01-02"), tc.want)
		}
	}
}

func TestCivilDateIn(t *testing.T) {
	taipei := taipeiLocation()
	// UTC 2026-06-11 18:00 = 台北 2026-06-12 02:00 → 日曆日應為 06-12。
	moment := time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC)
	got := civilDateIn(moment, taipei)
	if got.Format("2006-01-02") != "2026-06-12" {
		t.Errorf("civilDateIn = %s，預期 2026-06-12", got.Format("2006-01-02"))
	}
}
