package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
)

const (
	ledgerStatusDraft    = "draft"
	ledgerStatusComplete = "complete"
)

// LedgerRepository 约束账本的用户隔离读取、版本化草稿保存和事务式导入能力。
type LedgerRepository interface {
	ListLedgerRecords(context.Context, uuid.UUID, time.Time, time.Time) ([]model.LedgerRecord, error)
	CreateLedgerRecord(context.Context, *model.LedgerRecord) error
	SaveLedgerDraft(context.Context, uuid.UUID, uuid.UUID, int64, model.JSONB) (model.LedgerRecord, error)
	FindLedgerRecord(context.Context, uuid.UUID, uuid.UUID) (model.LedgerRecord, error)
	CompleteLedgerRecord(context.Context, uuid.UUID, uuid.UUID, int64, model.LedgerRecord) (model.LedgerRecord, error)
	DeleteLedgerRecord(context.Context, uuid.UUID, uuid.UUID) error
	ImportLedgerRecords(context.Context, uuid.UUID, []model.LedgerRecord) (int, int, error)
}

// LedgerFields 是自动保存草稿与正式账目共用的可编辑字段；金额始终使用十进制字符串传输。
type LedgerFields struct {
	Date          string `json:"date"`
	Time          string `json:"time"`
	Type          string `json:"type"`
	Amount        string `json:"amount"`
	Category      string `json:"category"`
	PaymentMethod string `json:"payment_method"`
	Flag          string `json:"flag"`
	Note          string `json:"note"`
}

// LedgerImportItem 支持业务 UUID 的幂等更新；旧 HTML 导出没有业务 UUID 时会作为新记录导入。
type LedgerImportItem struct {
	BusinessID string `json:"business_id"`
	LedgerFields
}

// LedgerImportResult 汇总批量导入写入数量，调用方可向用户显示导入结果。
type LedgerImportResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

// LedgerService 负责金额精度、草稿状态和记录归属规则，数据库查询只通过 repository 完成。
type LedgerService struct {
	records LedgerRepository
}

// NewLedgerService 装配账本业务服务。
func NewLedgerService(records LedgerRepository) *LedgerService {
	return &LedgerService{records: records}
}

// List 返回日期范围内的正式记录，并额外返回该用户所有可恢复草稿。
func (service *LedgerService) List(ctx context.Context, userID uuid.UUID, from, to string) ([]model.LedgerRecord, error) {
	start, err := parseLedgerDate(from)
	if err != nil {
		return nil, fault.InvalidField("from", "开始日期格式必须为 YYYY-MM-DD")
	}
	end, err := parseLedgerDate(to)
	if err != nil {
		return nil, fault.InvalidField("to", "结束日期格式必须为 YYYY-MM-DD")
	}
	if end.Before(start) {
		return nil, fault.InvalidRequest("结束日期不能早于开始日期")
	}
	return service.records.ListLedgerRecords(ctx, userID, start, end)
}

// CreateDraft 为新记录立即创建服务端草稿，不要求金额等必填内容已经完成。
func (service *LedgerService) CreateDraft(ctx context.Context, userID uuid.UUID, fields LedgerFields) (model.LedgerRecord, error) {
	clean, err := validateLedgerFields(fields, false)
	if err != nil {
		return model.LedgerRecord{}, err
	}
	draft, err := encodeLedgerDraft(clean)
	if err != nil {
		return model.LedgerRecord{}, fault.Internal(err)
	}
	date, _ := parseLedgerDate(clean.Date)
	record := model.LedgerRecord{
		BaseModel:    model.BaseModel{BusinessID: uuid.New()},
		UserID:       userID,
		RecordDate:   date,
		RecordType:   clean.Type,
		Status:       ledgerStatusDraft,
		DraftData:    &draft,
		DraftVersion: 1,
	}
	if err := service.records.CreateLedgerRecord(ctx, &record); err != nil {
		return model.LedgerRecord{}, err
	}
	return record, nil
}

// SaveDraft 将完整表单快照保存为新建草稿或已完成记录的待应用草稿。
func (service *LedgerService) SaveDraft(ctx context.Context, userID, recordID uuid.UUID, revision int64, fields LedgerFields) (model.LedgerRecord, error) {
	clean, err := validateLedgerFields(fields, false)
	if err != nil {
		return model.LedgerRecord{}, err
	}
	draft, err := encodeLedgerDraft(clean)
	if err != nil {
		return model.LedgerRecord{}, fault.Internal(err)
	}
	return service.records.SaveLedgerDraft(ctx, userID, recordID, revision, draft)
}

// Complete 在版本匹配且草稿有效时一次性应用全部字段；正式记录在此之前保持不变。
func (service *LedgerService) Complete(ctx context.Context, userID, recordID uuid.UUID, revision int64) (model.LedgerRecord, error) {
	record, err := service.records.FindLedgerRecord(ctx, userID, recordID)
	if err != nil {
		return model.LedgerRecord{}, err
	}
	if record.DraftVersion != revision {
		return model.LedgerRecord{}, fault.Conflict("记录已在其他操作中更新，请刷新后重试")
	}
	if record.DraftData == nil {
		return model.LedgerRecord{}, fault.Conflict("没有待完成的草稿")
	}
	var fields LedgerFields
	if err := json.Unmarshal(*record.DraftData, &fields); err != nil {
		return model.LedgerRecord{}, fault.Internal(fmt.Errorf("解析账本草稿失败: %w", err))
	}
	clean, err := validateLedgerFields(fields, true)
	if err != nil {
		return model.LedgerRecord{}, err
	}
	complete, err := completedLedgerModel(clean)
	if err != nil {
		return model.LedgerRecord{}, fault.InvalidField("amount", "金额格式无效")
	}
	return service.records.CompleteLedgerRecord(ctx, userID, recordID, revision, complete)
}

// Delete 只删除通过当前用户与业务 UUID 双重限定的记录。
func (service *LedgerService) Delete(ctx context.Context, userID, recordID uuid.UUID) error {
	return service.records.DeleteLedgerRecord(ctx, userID, recordID)
}

// Import 将已预览的完整记录在单个数据库事务中合并，不会删除文件未包含的记录。
func (service *LedgerService) Import(ctx context.Context, userID uuid.UUID, items []LedgerImportItem) (LedgerImportResult, error) {
	if len(items) == 0 || len(items) > 1000 {
		return LedgerImportResult{}, fault.InvalidField("records", "导入记录数量必须为 1 至 1000 条")
	}
	rows := make([]model.LedgerRecord, 0, len(items))
	for index, item := range items {
		clean, err := validateLedgerFields(item.LedgerFields, true)
		if err != nil {
			return LedgerImportResult{}, fault.InvalidField("records", fmt.Sprintf("第 %d 条记录无效", index+1))
		}
		row, err := completedLedgerModel(clean)
		if err != nil {
			return LedgerImportResult{}, fault.InvalidField("records", fmt.Sprintf("第 %d 条记录无效", index+1))
		}
		if parsed, parseErr := uuid.Parse(item.BusinessID); parseErr == nil {
			row.BusinessID = parsed
		}
		rows = append(rows, row)
	}
	created, updated, err := service.records.ImportLedgerRecords(ctx, userID, rows)
	if err != nil {
		return LedgerImportResult{}, err
	}
	return LedgerImportResult{Created: created, Updated: updated}, nil
}

// validateLedgerFields 校验稳定日期、本地时间、类型、标记及文本长度；草稿金额允许暂时为空。
func validateLedgerFields(fields LedgerFields, complete bool) (LedgerFields, error) {
	fields.Date = strings.TrimSpace(fields.Date)
	if _, err := parseLedgerDate(fields.Date); err != nil {
		return LedgerFields{}, fault.InvalidField("date", "日期格式必须为 YYYY-MM-DD")
	}
	if fields.Type != "income" && fields.Type != "expense" {
		return LedgerFields{}, fault.InvalidField("type", "收支类型必须为 income 或 expense")
	}
	if fields.Time != "" {
		if _, err := time.Parse("15:04", fields.Time); err != nil {
			return LedgerFields{}, fault.InvalidField("time", "时间格式必须为 HH:mm")
		}
	}
	fields.Category = strings.TrimSpace(fields.Category)
	fields.PaymentMethod = strings.TrimSpace(fields.PaymentMethod)
	fields.Note = strings.TrimSpace(fields.Note)
	if utf8.RuneCountInString(fields.Category) > 64 {
		return LedgerFields{}, fault.InvalidField("category", "分类不能超过 64 个字符")
	}
	if utf8.RuneCountInString(fields.PaymentMethod) > 64 {
		return LedgerFields{}, fault.InvalidField("payment_method", "支付方式不能超过 64 个字符")
	}
	if utf8.RuneCountInString(fields.Note) > 2000 {
		return LedgerFields{}, fault.InvalidField("note", "备注不能超过 2000 个字符")
	}
	switch fields.Flag {
	case "", "important", "anomaly", "pending_check":
	default:
		return LedgerFields{}, fault.InvalidField("flag", "标记值无效")
	}
	if _, err := parseLedgerAmount(fields.Amount, !complete); err != nil {
		return LedgerFields{}, fault.InvalidField("amount", "金额必须是非负且最多两位小数的数字")
	}
	return fields, nil
}

// ParseLedgerAmountFen 使用字符串转整数分，避免浮点运算造成金额精度误差。
func ParseLedgerAmountFen(amount string) (int64, error) {
	return parseLedgerAmount(amount, false)
}

// parseLedgerAmount 校验最多两位小数并使用整数分计算；allowPartial 仅用于字段自动保存草稿。
func parseLedgerAmount(amount string, allowPartial bool) (int64, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" && allowPartial {
		return 0, nil
	}
	if amount == "" || strings.HasPrefix(amount, "-") || strings.HasPrefix(amount, "+") {
		return 0, fmt.Errorf("金额为空或带有符号")
	}
	parts := strings.Split(amount, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts[0]) > 12 {
		return 0, fmt.Errorf("金额格式无效")
	}
	for _, char := range parts[0] {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("金额格式无效")
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > math.MaxInt64/100 {
		return 0, fmt.Errorf("金额超出范围")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" && !allowPartial || len(fraction) > 2 {
			return 0, fmt.Errorf("小数位数无效")
		}
		for _, char := range fraction {
			if char < '0' || char > '9' {
				return 0, fmt.Errorf("金额格式无效")
			}
		}
	}
	if len(fraction) == 1 {
		fraction += "0"
	}
	minor := int64(0)
	if fraction != "" {
		minor, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("金额格式无效")
		}
	}
	if whole*100 > math.MaxInt64-minor {
		return 0, fmt.Errorf("金额超出范围")
	}
	return whole*100 + minor, nil
}

// FormatLedgerAmountFen 将整数分稳定格式化为两位小数金额字符串。
func FormatLedgerAmountFen(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}

// parseLedgerDate 按日历日期解析，不将用户本地日期错误转换为相邻 UTC 日期。
func parseLedgerDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("日期格式无效")
	}
	return parsed.UTC(), nil
}

// encodeLedgerDraft 序列化本次完整表单快照；草稿只在显式完成后进入正式统计字段。
func encodeLedgerDraft(fields LedgerFields) (model.JSONB, error) {
	data, err := json.Marshal(fields)
	return model.JSONB(data), err
}

// completedLedgerModel 将校验后的正式条目转换为数据库字段。
func completedLedgerModel(fields LedgerFields) (model.LedgerRecord, error) {
	date, _ := parseLedgerDate(fields.Date)
	amountFen, err := parseLedgerAmount(fields.Amount, false)
	if err != nil {
		return model.LedgerRecord{}, err
	}
	var recordTime *time.Time
	if fields.Time != "" {
		parsed, parseErr := time.Parse("15:04", fields.Time)
		if parseErr != nil {
			return model.LedgerRecord{}, parseErr
		}
		value := time.Date(2000, 1, 1, parsed.Hour(), parsed.Minute(), 0, 0, time.UTC)
		recordTime = &value
	}
	return model.LedgerRecord{
		RecordDate: date, RecordTime: recordTime, RecordType: fields.Type, AmountFen: amountFen,
		Category: fields.Category, PaymentMethod: fields.PaymentMethod, Flag: fields.Flag, Note: fields.Note,
		Status: ledgerStatusComplete,
	}, nil
}
