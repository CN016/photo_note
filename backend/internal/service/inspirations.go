package service

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
)

// InspirationRepository 定义灵感图 CRUD、分页检索及标签业务 UUID 所有权校验接口。
type InspirationRepository interface {
	ValidateTagOwnership(context.Context, uuid.UUID, []uuid.UUID) error
	CreateInspiration(context.Context, *model.Inspiration) error
	ListInspirations(context.Context, uuid.UUID, []uuid.UUID, string, int, int) ([]model.Inspiration, int64, error)
	FindInspirationByBusinessID(context.Context, uuid.UUID, uuid.UUID) (model.Inspiration, error)
	UpdateInspiration(context.Context, uuid.UUID, uuid.UUID, map[string]any) (model.Inspiration, error)
	DeleteInspiration(context.Context, uuid.UUID, uuid.UUID) error
}

// InspirationService 校验编辑数据版本和标签归属，所有资源范围来自 access token 用户 UUID。
type InspirationService struct {
	inspirations InspirationRepository
}

// NewInspirationService 注入灵感图 repository。
func NewInspirationService(inspirations InspirationRepository) *InspirationService {
	return &InspirationService{inspirations: inspirations}
}

// InspirationInput 是创建或局部更新时已解析的业务数据，不包含 user_id 或数据库 id。
type InspirationInput struct {
	TagIDs   []uuid.UUID
	ImageKey string
	Data     json.RawMessage
}

// List 在数据库中按当前用户及 any/all 标签条件分页查询。
func (service *InspirationService) List(ctx context.Context, userID uuid.UUID, tagIDs []uuid.UUID, match string, page PageRequest) (Page[model.Inspiration], error) {
	if match == "" {
		match = "any"
	}
	if match != "any" && match != "all" {
		return Page[model.Inspiration]{}, fault.InvalidRequest("tag_match 只能是 any 或 all")
	}
	items, total, err := service.inspirations.ListInspirations(ctx, userID, tagIDs, match, page.Offset, page.PageSize)
	if err != nil {
		return Page[model.Inspiration]{}, err
	}
	return Page[model.Inspiration]{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// Create 检查 image_key、JSON 对象版本和标签归属后分配灵感图业务 UUID。
func (service *InspirationService) Create(ctx context.Context, userID uuid.UUID, input InspirationInput) (model.Inspiration, error) {
	input.ImageKey = strings.TrimSpace(input.ImageKey)
	if input.ImageKey == "" {
		return model.Inspiration{}, fault.InvalidField("image_key", "图片 key 不能为空")
	}
	if err := validateEditorData(input.Data); err != nil {
		return model.Inspiration{}, err
	}
	if err := service.inspirations.ValidateTagOwnership(ctx, userID, input.TagIDs); err != nil {
		return model.Inspiration{}, err
	}
	inspiration := model.Inspiration{
		BaseModel: model.BaseModel{BusinessID: uuid.New()},
		UserID:    userID,
		TagIDs:    uuidStrings(input.TagIDs),
		ImageKey:  input.ImageKey,
		Data:      model.JSONB(append([]byte(nil), input.Data...)),
	}
	if err := service.inspirations.CreateInspiration(ctx, &inspiration); err != nil {
		return model.Inspiration{}, err
	}
	return inspiration, nil
}

// Get 只允许从已认证用户的灵感图集合中按业务 UUID 读取详情。
func (service *InspirationService) Get(ctx context.Context, userID, inspirationID uuid.UUID) (model.Inspiration, error) {
	return service.inspirations.FindInspirationByBusinessID(ctx, userID, inspirationID)
}

// Update 保留未提交字段，显式空标签数组可清空关系，标签更新仍需通过归属校验。
func (service *InspirationService) Update(ctx context.Context, userID, inspirationID uuid.UUID, input InspirationInput, tagIDsSet, imageKeySet, dataSet bool) (model.Inspiration, error) {
	updates := make(map[string]any, 3)
	if tagIDsSet {
		if err := service.inspirations.ValidateTagOwnership(ctx, userID, input.TagIDs); err != nil {
			return model.Inspiration{}, err
		}
		// 使用 pq.StringArray 明确编码空数组为 PostgreSQL '{}'，避免驱动将空切片写成 NULL。
		updates["tag_ids"] = pq.StringArray(uuidStrings(input.TagIDs))
	}
	if imageKeySet {
		imageKey := strings.TrimSpace(input.ImageKey)
		if imageKey == "" {
			return model.Inspiration{}, fault.InvalidField("image_key", "图片 key 不能为空")
		}
		updates["image_key"] = imageKey
	}
	if dataSet {
		if err := validateEditorData(input.Data); err != nil {
			return model.Inspiration{}, err
		}
		updates["data"] = model.JSONB(append([]byte(nil), input.Data...))
	}
	if len(updates) == 0 {
		return model.Inspiration{}, fault.InvalidRequest("至少提供 tag_ids、image_key 或 data 中的一个字段")
	}
	return service.inspirations.UpdateInspiration(ctx, userID, inspirationID, updates)
}

// Delete 物理删除当前用户拥有的灵感图。
func (service *InspirationService) Delete(ctx context.Context, userID, inspirationID uuid.UUID) error {
	return service.inspirations.DeleteInspiration(ctx, userID, inspirationID)
}

// validateEditorData 只接受 JSON 对象并校验正整数 schema_version，保留其余编辑器字段原样存储。
func validateEditorData(data json.RawMessage) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return fault.InvalidField("data", "data 必须是有效的 JSON 对象")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil || object == nil {
		return fault.InvalidField("data", "data 必须是 JSON 对象")
	}
	versionJSON, exists := object["schema_version"]
	if !exists {
		return fault.InvalidField("data.schema_version", "data 必须包含正整数 schema_version")
	}
	var version json.Number
	decoder := json.NewDecoder(bytes.NewReader(versionJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&version); err != nil {
		return fault.InvalidField("data.schema_version", "schema_version 必须是正整数")
	}
	parsed, err := strconv.ParseInt(version.String(), 10, 64)
	if err != nil || parsed < 1 {
		return fault.InvalidField("data.schema_version", "schema_version 必须是正整数")
	}
	return nil
}

// uuidStrings 将业务 UUID 转为 PostgreSQL uuid[] 驱动可扫描的字符串数组。
func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.String())
	}
	return result
}
