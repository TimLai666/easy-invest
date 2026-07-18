package reconcile

// 券商庫存 CSV 解析器：per-broker plugin 設計。
// 每家券商一個 parser，實作 PositionCSVParser 後以 RegisterCSVParser 註冊；
// API 層依使用者選的格式名稱呼叫 ParsePositionsCSV，把券商檔案統一轉成 BrokerPositionInput
//（數量一律為「股」，與 docs/tw-market-rules.md 的單位規則一致）。
//
// TODO(insyra#188): 本解析器刻意用 encoding/csv + shopspring/decimal 自寫，而非
// insyra.ReadCSV。實測 Insyra v0.3.0 的欄位級型別推斷會把台股代號 0050 讀成 int64 50
// （開頭 0 遺失）、金額讀成 float64、空值變 NaN，會損毀財務資料，且無關閉推斷的選項。
// 待 insyra#188（no-inference / 讀為原始字串）上游支援後再評估採用。

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/shopspring/decimal"
)

// PositionCSVParser 單一券商匯出格式的解析器。
type PositionCSVParser interface {
	// Name 註冊與查詢用識別碼，例如 "generic"、"sinopac_dawho"。
	Name() string
	// Parse 解析 CSV 內容；輸出的 QuantityShares 一律為股。
	Parse(r io.Reader) ([]BrokerPositionInput, error)
}

var (
	csvParserMu sync.RWMutex
	csvParsers  = map[string]PositionCSVParser{}
)

func init() {
	RegisterCSVParser(genericCSVParser{})
	RegisterCSVParser(sinopacDawhoCSVParser{})
}

// RegisterCSVParser 註冊一個券商解析器；同名重複註冊以後者為準（方便測試替換）。
func RegisterCSVParser(p PositionCSVParser) {
	csvParserMu.Lock()
	defer csvParserMu.Unlock()
	csvParsers[p.Name()] = p
}

// LookupCSVParser 依名稱取得解析器。
func LookupCSVParser(name string) (PositionCSVParser, bool) {
	csvParserMu.RLock()
	defer csvParserMu.RUnlock()
	p, ok := csvParsers[name]
	return p, ok
}

// CSVParserNames 已註冊的解析器名稱（排序後），供 API 列出可用格式。
func CSVParserNames() []string {
	csvParserMu.RLock()
	defer csvParserMu.RUnlock()
	names := make([]string, 0, len(csvParsers))
	for name := range csvParsers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ParsePositionsCSV 依格式名稱解析券商庫存 CSV。
func ParsePositionsCSV(parserName string, r io.Reader) ([]BrokerPositionInput, error) {
	p, ok := LookupCSVParser(parserName)
	if !ok {
		return nil, fmt.Errorf("不支援的券商 CSV 格式 %q，可用格式：%s", parserName, strings.Join(CSVParserNames(), ", "))
	}
	return p.Parse(r)
}

// ---------- generic 格式 ----------

// genericCSVParser 內建通用格式：欄位 symbol,quantity_shares,avg_cost。
// 欄名大小寫不拘、檔案開頭可有 UTF-8 BOM、avg_cost 欄可省略或留空。
type genericCSVParser struct{}

func (genericCSVParser) Name() string { return "generic" }

func (genericCSVParser) Parse(r io.Reader) ([]BrokerPositionInput, error) {
	header, records, err := readCSVTable(r)
	if err != nil {
		return nil, err
	}
	symbolIdx := headerIndex(header, "symbol")
	qtyIdx := headerIndex(header, "quantity_shares", "quantity")
	costIdx := headerIndex(header, "avg_cost", "broker_avg_cost")
	if symbolIdx < 0 || qtyIdx < 0 {
		return nil, errors.New("generic 格式需要 symbol 與 quantity_shares 欄位")
	}
	var positions []BrokerPositionInput
	for i, rec := range records {
		symbol := strings.TrimSpace(cell(rec, symbolIdx))
		if symbol == "" {
			continue // 空列或缺代號的列直接略過
		}
		qty, err := parsePositiveDecimal(cell(rec, qtyIdx))
		if err != nil {
			return nil, fmt.Errorf("第 %d 列股數不合法：%w", i+2, err)
		}
		pos := BrokerPositionInput{Symbol: symbol, QuantityShares: qty.String()}
		if costIdx >= 0 {
			if raw := strings.TrimSpace(cell(rec, costIdx)); raw != "" {
				cost, err := parseNonNegativeDecimal(raw)
				if err != nil {
					return nil, fmt.Errorf("第 %d 列均價不合法：%w", i+2, err)
				}
				costText := cost.String()
				pos.BrokerAvgCost = &costText
			}
		}
		positions = append(positions, pos)
	}
	if len(positions) == 0 {
		return nil, errors.New("CSV 內沒有任何持股資料列")
	}
	return positions, nil
}

// ---------- 永豐金證券「大戶投」App 匯出格式 ----------

// sinopacDawhoCSVParser 永豐金證券大戶投 App 的庫存匯出格式。
//
// 假設（手上沒有官方格式文件，依常見匯出樣式定義，之後拿到真實檔案再校正）：
//   - UTF-8 編碼，開頭可帶 BOM；逗號分隔，第一列為欄名。
//   - 至少含「股票代號」「股票名稱」「集保庫存」「成交均價」四欄，其他欄位一律忽略。
//   - 「集保庫存」單位為股，數字可能帶千分位逗號（CSV 內以引號包住）。
//   - 「股票代號」欄可能夾帶名稱（如「2330 台積電」），取第一段空白前的代號。
//   - 合計列或空列的代號欄為空，直接略過。
type sinopacDawhoCSVParser struct{}

func (sinopacDawhoCSVParser) Name() string { return "sinopac_dawho" }

func (sinopacDawhoCSVParser) Parse(r io.Reader) ([]BrokerPositionInput, error) {
	header, records, err := readCSVTable(r)
	if err != nil {
		return nil, err
	}
	symbolIdx := headerIndex(header, "股票代號", "代號", "商品代號")
	nameIdx := headerIndex(header, "股票名稱", "商品名稱", "名稱")
	qtyIdx := headerIndex(header, "集保庫存", "庫存股數", "庫存(股)", "庫存")
	costIdx := headerIndex(header, "成交均價", "均價", "平均成本")
	if symbolIdx < 0 || qtyIdx < 0 {
		return nil, errors.New("大戶投格式需要「股票代號」與「集保庫存」欄位")
	}
	var positions []BrokerPositionInput
	for i, rec := range records {
		symbol := strings.TrimSpace(cell(rec, symbolIdx))
		if symbol == "" {
			continue // 合計列或空列
		}
		// 代號欄夾帶名稱時只取代號本身。
		if fields := strings.Fields(symbol); len(fields) > 0 {
			symbol = fields[0]
		}
		qty, err := parsePositiveDecimal(cell(rec, qtyIdx))
		if err != nil {
			return nil, fmt.Errorf("第 %d 列集保庫存不合法：%w", i+2, err)
		}
		pos := BrokerPositionInput{Symbol: symbol, QuantityShares: qty.String()}
		if costIdx >= 0 {
			if raw := strings.TrimSpace(cell(rec, costIdx)); raw != "" {
				cost, err := parseNonNegativeDecimal(raw)
				if err != nil {
					return nil, fmt.Errorf("第 %d 列成交均價不合法：%w", i+2, err)
				}
				costText := cost.String()
				pos.BrokerAvgCost = &costText
			}
		}
		if nameIdx >= 0 {
			if name := strings.TrimSpace(cell(rec, nameIdx)); name != "" {
				pos.Details = map[string]any{"name": name}
			}
		}
		positions = append(positions, pos)
	}
	if len(positions) == 0 {
		return nil, errors.New("CSV 內沒有任何持股資料列")
	}
	return positions, nil
}

// ---------- 共用工具 ----------

const utf8BOM = "\xEF\xBB\xBF"

// readCSVTable 讀整份 CSV，去掉開頭 BOM，回傳欄名列與資料列。
// 各列欄數允許不一致（部分券商匯出會在最後補逗號）。
func readCSVTable(r io.Reader) (header []string, records [][]string, err error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("CSV 解析失敗：%w", err)
	}
	if len(rows) == 0 {
		return nil, nil, errors.New("CSV 內容為空")
	}
	header = rows[0]
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], utf8BOM)
	}
	return header, rows[1:], nil
}

// headerIndex 在欄名列中找出第一個符合任一別名的欄位（忽略大小寫與前後空白）。
func headerIndex(header []string, aliases ...string) int {
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		for _, alias := range aliases {
			if name == strings.ToLower(alias) {
				return i
			}
		}
	}
	return -1
}

// cell 安全取出第 idx 欄，超出範圍回空字串。
func cell(rec []string, idx int) string {
	if idx < 0 || idx >= len(rec) {
		return ""
	}
	return rec[idx]
}

// parsePositiveDecimal 解析必須大於 0 的數字，容忍千分位逗號與前後空白。
func parsePositiveDecimal(raw string) (decimal.Decimal, error) {
	d, err := parseNonNegativeDecimal(raw)
	if err != nil {
		return decimal.Zero, err
	}
	if d.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("數量必須大於 0，收到 %q", raw)
	}
	return d, nil
}

// parseNonNegativeDecimal 解析不可為負的數字，容忍千分位逗號與前後空白。
func parseNonNegativeDecimal(raw string) (decimal.Decimal, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	if cleaned == "" {
		return decimal.Zero, errors.New("數字欄位為空")
	}
	d, err := decimal.NewFromString(cleaned)
	if err != nil {
		return decimal.Zero, fmt.Errorf("無法解析數字 %q", raw)
	}
	if d.IsNegative() {
		return decimal.Zero, fmt.Errorf("數字不可為負，收到 %q", raw)
	}
	return d, nil
}
