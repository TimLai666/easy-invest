package marketdata

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

const (
	backoffBase = 1 * time.Minute
	backoffCap  = 1 * time.Hour
	// maxConsecutiveFails 連續失敗達上限後，該來源標記為暫停抓取，等下個排程週期再試。
	maxConsecutiveFails = 6
)

// SourceLimiter 是「每個來源一個」的全域節流器：同一來源的所有請求
// （不分 dataset、不分 goroutine）共用同一個實例，序列化所有等待。
// 收到 429/403/HTML 錯誤頁等異常時走指數退避（基準 1 分鐘、上限 1 小時、加 jitter）。
type SourceLimiter struct {
	name     string
	interval time.Duration
	jitter   time.Duration

	mu               sync.Mutex
	nextAllowed      time.Time
	consecutiveFails int
	pausedUntil      time.Time
}

// NewSourceLimiter 建立節流器。interval 是兩個請求之間的最小間隔，
// 實際等待時間是 interval 加 0〜jitter 的隨機值。
func NewSourceLimiter(name string, interval, jitter time.Duration) *SourceLimiter {
	return &SourceLimiter{name: name, interval: interval, jitter: jitter}
}

// Wait 阻塞到允許發出下一個請求為止；context 取消時提前返回錯誤。
func (l *SourceLimiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		wakeAt := l.nextAllowed
		if l.pausedUntil.After(wakeAt) {
			wakeAt = l.pausedUntil
		}
		if !wakeAt.After(now) {
			// 預約下一個允許時間後放行。
			delay := l.interval
			if l.jitter > 0 {
				delay += time.Duration(rand.Int63n(int64(l.jitter)))
			}
			l.nextAllowed = now.Add(delay)
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()

		timer := time.NewTimer(wakeAt.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// ReportSuccess 清除退避狀態。
func (l *SourceLimiter) ReportSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consecutiveFails = 0
	l.pausedUntil = time.Time{}
}

// ReportFailure 記錄一次來源異常並進入指數退避；回傳目前連續失敗次數。
func (l *SourceLimiter) ReportFailure() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consecutiveFails++
	delay := backoffDelay(l.consecutiveFails)
	if l.jitter > 0 {
		delay += time.Duration(rand.Int63n(int64(l.jitter)))
	}
	l.pausedUntil = time.Now().Add(delay)
	return l.consecutiveFails
}

// Paused 回傳來源是否因連續失敗達上限而應暫停抓取（等下個排程週期再試）。
func (l *SourceLimiter) Paused() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.consecutiveFails >= maxConsecutiveFails
}

// backoffDelay 計算第 n 次連續失敗的退避時間：基準 1 分鐘指數成長、上限 1 小時。
func backoffDelay(fails int) time.Duration {
	if fails < 1 {
		return 0
	}
	delay := backoffBase
	for i := 1; i < fails; i++ {
		delay *= 2
		if delay >= backoffCap {
			return backoffCap
		}
	}
	if delay > backoffCap {
		return backoffCap
	}
	return delay
}
