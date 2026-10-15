package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
)

// ledgerRecordResponse 显式暴露账本业务字段，不包含自增 ID、用户 UUID 或数据库草稿原始字节。
type ledgerRecordResponse struct {
	BusinessID    uuid.UUID             `json:"business_id"`
	Date          string                `json:"date"`
	Time          string                `json:"time"`
	Type          string                `json:"type"`
	Amount        string                `json:"amount"`
	Category      string                `json:"category"`
	PaymentMethod string                `json:"payment_method"`
	Flag          string                `json:"flag"`
	Note          string                `json:"note"`
	Status        string                `json:"status"`
	Draft         *service.LedgerFields `json:"draft"`
	DraftVersion  int64                 `json:"draft_version"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

// ledgerRecordDTO 将数据库时间和整数分映射为稳定的日历日期、本地时间与十进制金额字符串。
func ledgerRecordDTO(record model.LedgerRecord) ledgerRecordResponse {
	clock := ""
	if record.RecordTime != nil {
		clock = record.RecordTime.Format("15:04")
	}
	var draft *service.LedgerFields
	if record.DraftData != nil {
		var fields service.LedgerFields
		if json.Unmarshal(*record.DraftData, &fields) == nil {
			draft = &fields
		}
	}
	return ledgerRecordResponse{
		BusinessID: record.BusinessID, Date: record.RecordDate.Format("2006-01-02"), Time: clock,
		Type: record.RecordType, Amount: service.FormatLedgerAmountFen(record.AmountFen),
		Category: record.Category, PaymentMethod: record.PaymentMethod, Flag: record.Flag,
		Note: record.Note, Status: record.Status, Draft: draft, DraftVersion: record.DraftVersion,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

// listLedgerRecordsHandler 只列出当前 token 对应用户的记录，并要求明确的日历日期范围。
func listLedgerRecordsHandler(ledger *service.LedgerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if c.Query("from") == "" || c.Query("to") == "" {
			writeError(c, fault.InvalidRequest("必须提供 from 和 to 日期"))
			return
		}
		records, err := ledger.List(c.Request.Context(), claims.UserBusinessID, c.Query("from"), c.Query("to"))
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]ledgerRecordResponse, 0, len(records))
		for _, record := range records {
			items = append(items, ledgerRecordDTO(record))
		}
		writeData(c, http.StatusOK, gin.H{"items": items})
	}
}

// createLedgerDraftHandler 为当前用户建立初始草稿并立即返回业务 UUID 与修订号。
func createLedgerDraftHandler(ledger *service.LedgerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var fields service.LedgerFields
		if err := decodeJSON(c, &fields); err != nil {
			writeError(c, err)
			return
		}
		record, err := ledger.CreateDraft(c.Request.Context(), claims.UserBusinessID, fields)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, ledgerRecordDTO(record))
	}
}

// saveLedgerDraftHandler 用客户端修订号防止网络迟到请求覆盖新输入。
func saveLedgerDraftHandler(ledger *service.LedgerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		recordID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var request struct {
			Revision int64 `json:"revision"`
			service.LedgerFields
		}
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		record, err := ledger.SaveDraft(c.Request.Context(), claims.UserBusinessID, recordID, request.Revision, request.LedgerFields)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, ledgerRecordDTO(record))
	}
}

// completeLedgerRecordHandler 校验并应用当前草稿；正式数据只在该请求成功后变化。
func completeLedgerRecordHandler(ledger *service.LedgerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		recordID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var request struct {
			Revision int64 `json:"revision"`
		}
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		record, err := ledger.Complete(c.Request.Context(), claims.UserBusinessID, recordID, request.Revision)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, ledgerRecordDTO(record))
	}
}

// deleteLedgerRecordHandler 按用户业务 UUID 删除目标记录或待完成草稿。
func deleteLedgerRecordHandler(ledger *service.LedgerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		recordID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := ledger.Delete(c.Request.Context(), claims.UserBusinessID, recordID); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// importLedgerRecordsHandler 在服务层校验后以单个数据库事务合并导入记录。
func importLedgerRecordsHandler(ledger *service.LedgerService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var request struct {
			Records []service.LedgerImportItem `json:"records"`
		}
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		result, err := ledger.Import(c.Request.Context(), claims.UserBusinessID, request.Records)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, result)
	}
}
