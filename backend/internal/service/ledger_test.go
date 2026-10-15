package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
)

type memoryLedgerRepository struct {
	records map[uuid.UUID]model.LedgerRecord
}

func newMemoryLedgerRepository() *memoryLedgerRepository {
	return &memoryLedgerRepository{records: make(map[uuid.UUID]model.LedgerRecord)}
}

func (repository *memoryLedgerRepository) ListLedgerRecords(_ context.Context, userID uuid.UUID, from, to time.Time) ([]model.LedgerRecord, error) {
	items := make([]model.LedgerRecord, 0)
	for _, record := range repository.records {
		if record.UserID == userID && (!record.RecordDate.Before(from) && !record.RecordDate.After(to) || record.Status == "draft" || record.DraftData != nil) {
			items = append(items, record)
		}
	}
	return items, nil
}

func (repository *memoryLedgerRepository) CreateLedgerRecord(_ context.Context, record *model.LedgerRecord) error {
	record.CreatedAt = time.Now().UTC()
	record.UpdatedAt = record.CreatedAt
	repository.records[record.BusinessID] = *record
	return nil
}

func (repository *memoryLedgerRepository) SaveLedgerDraft(_ context.Context, userID, recordID uuid.UUID, revision int64, draft model.JSONB) (model.LedgerRecord, error) {
	record, err := repository.FindLedgerRecord(context.Background(), userID, recordID)
	if err != nil {
		return model.LedgerRecord{}, err
	}
	if record.DraftVersion != revision {
		return model.LedgerRecord{}, fault.Conflict("版本冲突")
	}
	record.DraftData = &draft
	record.DraftVersion++
	record.UpdatedAt = time.Now().UTC()
	repository.records[recordID] = record
	return record, nil
}

func (repository *memoryLedgerRepository) FindLedgerRecord(_ context.Context, userID, recordID uuid.UUID) (model.LedgerRecord, error) {
	record, ok := repository.records[recordID]
	if !ok || record.UserID != userID {
		return model.LedgerRecord{}, fault.NotFound()
	}
	return record, nil
}

func (repository *memoryLedgerRepository) CompleteLedgerRecord(_ context.Context, userID, recordID uuid.UUID, revision int64, complete model.LedgerRecord) (model.LedgerRecord, error) {
	record, err := repository.FindLedgerRecord(context.Background(), userID, recordID)
	if err != nil {
		return model.LedgerRecord{}, err
	}
	if record.DraftVersion != revision || record.DraftData == nil {
		return model.LedgerRecord{}, fault.Conflict("版本冲突")
	}
	record.RecordDate = complete.RecordDate
	record.RecordTime = complete.RecordTime
	record.RecordType = complete.RecordType
	record.AmountFen = complete.AmountFen
	record.Category = complete.Category
	record.PaymentMethod = complete.PaymentMethod
	record.Flag = complete.Flag
	record.Note = complete.Note
	record.Status = "complete"
	record.DraftData = nil
	record.DraftVersion++
	repository.records[recordID] = record
	return record, nil
}

func (repository *memoryLedgerRepository) DeleteLedgerRecord(_ context.Context, userID, recordID uuid.UUID) error {
	if _, err := repository.FindLedgerRecord(context.Background(), userID, recordID); err != nil {
		return err
	}
	delete(repository.records, recordID)
	return nil
}

func (repository *memoryLedgerRepository) ImportLedgerRecords(_ context.Context, userID uuid.UUID, rows []model.LedgerRecord) (created, updated int, err error) {
	for _, row := range rows {
		if current, ok := repository.records[row.BusinessID]; ok && current.UserID == userID {
			row.UserID = userID
			row.DraftVersion = current.DraftVersion + 1
			updated++
		} else {
			row.BusinessID = uuid.New()
			row.UserID = userID
			created++
		}
		row.Status = "complete"
		row.DraftData = nil
		repository.records[row.BusinessID] = row
	}
	return created, updated, nil
}

func TestParseLedgerAmountFen(t *testing.T) {
	tests := []struct {
		value string
		want  int64
		valid bool
	}{
		{value: "0", want: 0, valid: true},
		{value: "12.3", want: 1230, valid: true},
		{value: "0012.30", want: 1230, valid: true},
		{value: "12.345", valid: false},
		{value: "-1", valid: false},
		{value: "1e3", valid: false},
		{value: "", valid: false},
	}
	for _, test := range tests {
		got, err := ParseLedgerAmountFen(test.value)
		if (err == nil) != test.valid || (err == nil && got != test.want) {
			t.Errorf("ParseLedgerAmountFen(%q) = %d, %v; want %d, valid=%v", test.value, got, err, test.want, test.valid)
		}
	}
	if got := FormatLedgerAmountFen(1230); got != "12.30" {
		t.Fatalf("FormatLedgerAmountFen(1230) = %q", got)
	}
}

func TestLedgerDraftCompletesWithoutChangingFormalRecordEarly(t *testing.T) {
	repository := newMemoryLedgerRepository()
	service := NewLedgerService(repository)
	userID := uuid.New()

	fields := LedgerFields{Date: "2026-10-15", Type: "income", Amount: "", Category: "门店", PaymentMethod: "wechat"}
	draft, err := service.CreateDraft(context.Background(), userID, fields)
	if err != nil {
		t.Fatal(err)
	}
	fields.Amount = "1288.88"
	draft, err = service.SaveDraft(context.Background(), userID, draft.BusinessID, draft.DraftVersion, fields)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != "draft" || draft.DraftData == nil || draft.AmountFen != 0 {
		t.Fatalf("draft changed formal amount before completion: %#v", draft)
	}
	completed, err := service.Complete(context.Background(), userID, draft.BusinessID, draft.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "complete" || completed.AmountFen != 128888 || completed.DraftData != nil {
		t.Fatalf("completed record has wrong state: %#v", completed)
	}
}

func TestLedgerEditKeepsOriginalUntilCompleteAndScopesByUser(t *testing.T) {
	repository := newMemoryLedgerRepository()
	service := NewLedgerService(repository)
	userID := uuid.New()
	otherUser := uuid.New()
	initial := LedgerFields{Date: "2026-10-14", Type: "expense", Amount: "50.00", Category: "采购", PaymentMethod: "cash"}
	complete, err := completedLedgerModel(initial)
	if err != nil {
		t.Fatal(err)
	}
	complete.BaseModel.BusinessID = uuid.New()
	complete.UserID = userID
	complete.CreatedAt = time.Now().UTC()
	complete.DraftVersion = 4
	draftJSON, _ := json.Marshal(initial)
	draftData := model.JSONB(draftJSON)
	complete.DraftData = &draftData
	complete.Status = "complete"
	repository.records[complete.BusinessID] = complete

	updatedFields := initial
	updatedFields.Amount = "62.35"
	updatedFields.Date = "2026-10-15"
	saved, err := service.SaveDraft(context.Background(), userID, complete.BusinessID, complete.DraftVersion, updatedFields)
	if err != nil {
		t.Fatal(err)
	}
	if saved.AmountFen != 5000 || saved.Status != "complete" {
		t.Fatalf("formal record changed before completion: %#v", saved)
	}
	if _, err := service.Complete(context.Background(), otherUser, complete.BusinessID, saved.DraftVersion); !fault.Is(err, 404, "not_found") {
		t.Fatalf("cross-user completion error = %v", err)
	}
	final, err := service.Complete(context.Background(), userID, complete.BusinessID, saved.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	if final.AmountFen != 6235 || final.RecordDate.Format("2006-01-02") != "2026-10-15" {
		t.Fatalf("completed edit = %#v", final)
	}
}

func TestLedgerImportMergesOnlyMatchingBusinessIDs(t *testing.T) {
	repository := newMemoryLedgerRepository()
	service := NewLedgerService(repository)
	userID := uuid.New()
	otherUser := uuid.New()
	existingID := uuid.New()
	current, _ := completedLedgerModel(LedgerFields{Date: "2026-10-01", Type: "income", Amount: "10.00"})
	current.BusinessID = existingID
	current.UserID = userID
	repository.records[existingID] = current
	foreign, _ := completedLedgerModel(LedgerFields{Date: "2026-10-02", Type: "income", Amount: "20.00"})
	foreign.BusinessID = uuid.New()
	foreign.UserID = otherUser
	repository.records[foreign.BusinessID] = foreign

	result, err := service.Import(context.Background(), userID, []LedgerImportItem{
		{BusinessID: existingID.String(), LedgerFields: LedgerFields{Date: "2026-10-01", Type: "income", Amount: "11.00"}},
		{BusinessID: foreign.BusinessID.String(), LedgerFields: LedgerFields{Date: "2026-10-03", Type: "expense", Amount: "5.00"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || result.Updated != 1 {
		t.Fatalf("import result = %#v", result)
	}
	if repository.records[existingID].AmountFen != 1100 || repository.records[foreign.BusinessID].UserID != otherUser {
		t.Fatal("import crossed account ownership or failed to update the matching record")
	}
}

func TestLedgerRejectsInvalidAmountAndDate(t *testing.T) {
	repository := newMemoryLedgerRepository()
	service := NewLedgerService(repository)
	userID := uuid.New()
	if _, err := service.CreateDraft(context.Background(), userID, LedgerFields{Date: "2026-02-30", Type: "income"}); err == nil {
		t.Fatal("expected invalid calendar date to fail")
	}
	if _, err := service.Import(context.Background(), userID, []LedgerImportItem{{LedgerFields: LedgerFields{Date: "2026-10-15", Type: "expense", Amount: "1.001"}}}); err == nil {
		t.Fatal("expected amount with more than two decimal places to fail")
	}
}
