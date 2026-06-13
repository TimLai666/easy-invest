package reconcile

import (
	"strings"
	"testing"
)

func TestGenericCSVParser(t *testing.T) {
	t.Run("含 BOM、欄名大小寫不拘、avg_cost 可留空", func(t *testing.T) {
		csv := "\xEF\xBB\xBFSymbol,Quantity_Shares,AVG_COST\n" +
			"2330,1000,600.855\n" +
			"0050,2000,\n" +
			"\n" +
			"00878,1500,21.5\n"
		positions, err := ParsePositionsCSV("generic", strings.NewReader(csv))
		if err != nil {
			t.Fatal(err)
		}
		if len(positions) != 3 {
			t.Fatalf("len(positions) = %d, want 3", len(positions))
		}
		if positions[0].Symbol != "2330" || positions[0].QuantityShares != "1000" {
			t.Fatalf("第一筆解析錯誤：%+v", positions[0])
		}
		if positions[0].BrokerAvgCost == nil || *positions[0].BrokerAvgCost != "600.855" {
			t.Fatalf("第一筆均價解析錯誤：%+v", positions[0].BrokerAvgCost)
		}
		if positions[1].BrokerAvgCost != nil {
			t.Fatalf("空白均價應為 nil，得到 %v", *positions[1].BrokerAvgCost)
		}
		if positions[2].Symbol != "00878" || *positions[2].BrokerAvgCost != "21.5" {
			t.Fatalf("第三筆解析錯誤：%+v", positions[2])
		}
	})

	t.Run("缺少必要欄位回報錯誤", func(t *testing.T) {
		if _, err := ParsePositionsCSV("generic", strings.NewReader("symbol,price\n2330,600\n")); err == nil {
			t.Fatal("缺 quantity_shares 欄位應該要報錯")
		}
	})

	t.Run("股數不合法回報錯誤", func(t *testing.T) {
		if _, err := ParsePositionsCSV("generic", strings.NewReader("symbol,quantity_shares\n2330,abc\n")); err == nil {
			t.Fatal("股數非數字應該要報錯")
		}
		if _, err := ParsePositionsCSV("generic", strings.NewReader("symbol,quantity_shares\n2330,0\n")); err == nil {
			t.Fatal("股數為 0 應該要報錯")
		}
	})

	t.Run("沒有任何資料列回報錯誤", func(t *testing.T) {
		if _, err := ParsePositionsCSV("generic", strings.NewReader("symbol,quantity_shares,avg_cost\n")); err == nil {
			t.Fatal("只有欄名沒有資料應該要報錯")
		}
	})
}

func TestSinopacDawhoCSVParser(t *testing.T) {
	t.Run("中文欄名、BOM、千分位與名稱欄", func(t *testing.T) {
		csv := "\xEF\xBB\xBF股票代號,股票名稱,集保庫存,成交均價,參考損益\n" +
			"2330,台積電,\"1,000\",600.86,12345\n" +
			"0050,元大台灣50,2000,100.14,678\n" +
			",合計,,,13023\n"
		positions, err := ParsePositionsCSV("sinopac_dawho", strings.NewReader(csv))
		if err != nil {
			t.Fatal(err)
		}
		if len(positions) != 2 {
			t.Fatalf("len(positions) = %d, want 2（合計列要略過）", len(positions))
		}
		if positions[0].Symbol != "2330" || positions[0].QuantityShares != "1000" {
			t.Fatalf("第一筆解析錯誤：%+v", positions[0])
		}
		if positions[0].BrokerAvgCost == nil || *positions[0].BrokerAvgCost != "600.86" {
			t.Fatalf("第一筆均價解析錯誤：%+v", positions[0].BrokerAvgCost)
		}
		if name, _ := positions[0].Details["name"].(string); name != "台積電" {
			t.Fatalf("details.name = %q, want 台積電", name)
		}
	})

	t.Run("代號欄夾帶名稱時只取代號", func(t *testing.T) {
		csv := "股票代號,集保庫存,成交均價\n2330 台積電,1000,600.86\n"
		positions, err := ParsePositionsCSV("sinopac_dawho", strings.NewReader(csv))
		if err != nil {
			t.Fatal(err)
		}
		if positions[0].Symbol != "2330" {
			t.Fatalf("symbol = %q, want 2330", positions[0].Symbol)
		}
	})

	t.Run("缺少必要欄位回報錯誤", func(t *testing.T) {
		if _, err := ParsePositionsCSV("sinopac_dawho", strings.NewReader("股票名稱,成交均價\n台積電,600\n")); err == nil {
			t.Fatal("缺「股票代號」「集保庫存」應該要報錯")
		}
	})
}

func TestCSVParserRegistry(t *testing.T) {
	if _, err := ParsePositionsCSV("unknown_broker", strings.NewReader("a,b\n1,2\n")); err == nil {
		t.Fatal("未註冊的格式應該要報錯")
	}
	names := CSVParserNames()
	wantNames := map[string]bool{"generic": false, "sinopac_dawho": false}
	for _, name := range names {
		if _, ok := wantNames[name]; ok {
			wantNames[name] = true
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Fatalf("內建解析器 %q 未註冊，現有：%v", name, names)
		}
	}
}
