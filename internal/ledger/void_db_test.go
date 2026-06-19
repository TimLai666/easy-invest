package ledger

// DB 整合測試：void 一筆買進事件後，批次應從事件流重建（該資產批次清空），
// 且不報錯。需要 TEST_DATABASE_URL。

import (
	"context"
	"testing"
)

func TestVoidBuyRebuildsLotsWithDatabase(t *testing.T) {
	pool := portfolioHistoryTestPool(t)
	ctx := context.Background()
	userID := setupPortfolioHistoryUser(t, ctx, pool)
	svc := NewService(pool)

	deposit := "1000000"
	if _, err := svc.CreateEvent(ctx, CreateEventInput{
		UserID: userID, EventType: EventCashDeposit, TradeDate: "2026-01-02", GrossAmount: &deposit,
	}); err != nil {
		t.Fatalf("入金失敗：%v", err)
	}
	buy, err := svc.CreateEvent(ctx, CreateEventInput{
		UserID: userID, EventType: EventBuy, Symbol: "2330", TradeDate: "2026-01-05",
		Quantity: "1000", Unit: "share", Price: "600",
	})
	if err != nil {
		t.Fatalf("買進失敗：%v", err)
	}

	lots, err := svc.Lots(ctx, userID, "2330")
	if err != nil {
		t.Fatalf("查批次失敗：%v", err)
	}
	if len(lots) != 1 {
		t.Fatalf("買進後 2330 批次數 = %d, want 1", len(lots))
	}

	// 作廢買進 → 應重建批次，2330 不再有未平倉批次，且不報錯。
	if err := svc.VoidEvent(ctx, userID, buy.ID, "測試作廢"); err != nil {
		t.Fatalf("作廢失敗：%v", err)
	}
	after, err := svc.Lots(ctx, userID, "2330")
	if err != nil {
		t.Fatalf("作廢後查批次失敗：%v", err)
	}
	if len(after) != 0 {
		t.Fatalf("作廢後 2330 批次數 = %d, want 0", len(after))
	}
}
