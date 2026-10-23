package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"golang.org/x/text/unicode/norm"
)

const projectDocumentSchemaVersion = 1

// ProjectRepository 封装项目 CRUD、短中文全文检索和两个项目资源关联表的事务同步。
type ProjectRepository interface {
	ListProjects(context.Context, uuid.UUID, string, int, int) ([]model.Project, int64, error)
	FindProject(context.Context, uuid.UUID, uuid.UUID) (model.Project, error)
	CreateProject(context.Context, *model.Project, []uuid.UUID, []model.ProjectAttachment) error
	UpdateProject(context.Context, uuid.UUID, uuid.UUID, int64, map[string]any, []uuid.UUID, []model.ProjectAttachment) (model.Project, error)
	DeleteProject(context.Context, uuid.UUID, uuid.UUID) error
	FindProjectAttachment(context.Context, uuid.UUID, uuid.UUID, string) (model.ProjectAttachment, error)
	ValidateProjectInspirations(context.Context, uuid.UUID, []uuid.UUID) error
}

// ProjectFileReader 只读取现有鉴权存储文件，返回实际大小和识别出的内容类型。
type ProjectFileReader interface {
	Open(string) (io.ReadCloser, int64, string, error)
}

// ProjectService 负责项目正文结构、资源引用和标题/正文搜索文本校验。
type ProjectService struct {
	repository ProjectRepository
	files      ProjectFileReader
	maxBytes   int64
}

// NewProjectService 注入项目 repository、现有文件存储读取能力和服务端上传上限。
func NewProjectService(repository ProjectRepository, files ProjectFileReader, maxBytes int64) *ProjectService {
	return &ProjectService{repository: repository, files: files, maxBytes: maxBytes}
}

// ProjectSave 是创建和自动保存共用的完整文档快照；更新时 revision 必须与服务端一致。
type ProjectSave struct {
	Title                 string          `json:"title"`
	CoverEmoji            string          `json:"cover_emoji"`
	CoverImageKey         string          `json:"cover_image_key"`
	CoverFileName         string          `json:"cover_file_name"`
	DocumentSchemaVersion int             `json:"document_schema_version"`
	Document              json.RawMessage `json:"document"`
	Revision              int64           `json:"revision"`
}

// ProjectView 只暴露项目业务字段和关联资源，不返回自增 ID、所属用户或索引字段。
type ProjectView struct {
	BusinessID            uuid.UUID       `json:"business_id"`
	Title                 string          `json:"title"`
	CoverEmoji            string          `json:"cover_emoji"`
	CoverImageKey         string          `json:"cover_image_key"`
	CoverFileName         string          `json:"cover_file_name"`
	DocumentSchemaVersion int16           `json:"document_schema_version"`
	Document              json.RawMessage `json:"document"`
	Revision              int64           `json:"revision"`
	CreatedAt             string          `json:"created_at"`
	UpdatedAt             string          `json:"updated_at"`
}

// ProjectSummary 用于项目卡片列表，避免分页响应下载每个文档的完整 JSONB 正文。
type ProjectSummary struct {
	BusinessID    uuid.UUID `json:"business_id"`
	Title         string    `json:"title"`
	CoverEmoji    string    `json:"cover_emoji"`
	CoverImageKey string    `json:"cover_image_key"`
	CoverFileName string    `json:"cover_file_name"`
	Revision      int64     `json:"revision"`
	CreatedAt     string    `json:"created_at"`
	UpdatedAt     string    `json:"updated_at"`
}

// ProjectAttachmentView 是项目授权范围内可预览的附件元数据。
type ProjectAttachmentView struct {
	BusinessID  uuid.UUID `json:"business_id"`
	FileName    string    `json:"file_name"`
	FileSize    int64     `json:"file_size"`
	ContentType string    `json:"content_type"`
	StoreKey    string    `json:"storekey"`
}

// List 验证搜索分页参数并返回当前用户拥有的项目。
func (service *ProjectService) List(ctx context.Context, userID uuid.UUID, query string, page PageRequest) (Page[ProjectSummary], error) {
	query = normalizeProjectText(query)
	if utf8.RuneCountInString(query) > 120 {
		return Page[ProjectSummary]{}, fault.InvalidRequest("搜索内容最多 120 个字符")
	}
	items, total, err := service.repository.ListProjects(ctx, userID, query, page.Offset, page.PageSize)
	if err != nil {
		return Page[ProjectSummary]{}, err
	}
	views := make([]ProjectSummary, 0, len(items))
	for _, item := range items {
		views = append(views, projectSummary(item))
	}
	return Page[ProjectSummary]{Items: views, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// Get 隐藏其他用户与已软删除项目的存在状态。
func (service *ProjectService) Get(ctx context.Context, userID, projectID uuid.UUID) (ProjectView, error) {
	item, err := service.repository.FindProject(ctx, userID, projectID)
	if err != nil {
		return ProjectView{}, err
	}
	return projectView(item), nil
}

// Create 校验富文本文档和所有资源后创建项目，并在同一 repository 事务中登记引用。
func (service *ProjectService) Create(ctx context.Context, userID uuid.UUID, input ProjectSave) (ProjectView, error) {
	project, inspirations, attachments, err := service.prepare(ctx, userID, input)
	if err != nil {
		return ProjectView{}, err
	}
	project.BaseModel.BusinessID = uuid.New()
	project.UserID = userID
	project.Revision = 1
	if err := service.repository.CreateProject(ctx, &project, inspirations, attachments); err != nil {
		return ProjectView{}, err
	}
	return projectView(project), nil
}

// Update 使用期望修订号阻止迟到的自动保存覆盖更新内容。
func (service *ProjectService) Update(ctx context.Context, userID, projectID uuid.UUID, input ProjectSave) (ProjectView, error) {
	if input.Revision < 1 {
		return ProjectView{}, fault.InvalidField("revision", "拍摄策划修订号无效")
	}
	project, inspirations, attachments, err := service.prepare(ctx, userID, input)
	if err != nil {
		return ProjectView{}, err
	}
	updates := map[string]any{
		"title": project.Title, "cover_emoji": project.CoverEmoji,
		"cover_image_key": project.CoverImageKey, "cover_file_name": project.CoverFileName,
		"document_schema_version": project.DocumentSchemaVersion, "document": project.Document,
		"search_text": project.SearchText, "search_grams": project.SearchGrams,
	}
	updated, err := service.repository.UpdateProject(ctx, userID, projectID, input.Revision, updates, inspirations, attachments)
	if err != nil {
		return ProjectView{}, err
	}
	return projectView(updated), nil
}

// Delete 软删除项目；资源关联继续保留，以便删除的项目仍阻止其灵感图被清除。
func (service *ProjectService) Delete(ctx context.Context, userID, projectID uuid.UUID) error {
	return service.repository.DeleteProject(ctx, userID, projectID)
}

// Attachment 查询当前用户项目中的附件并再次确认底层文件仍可读。
func (service *ProjectService) Attachment(ctx context.Context, userID, projectID uuid.UUID, storeKey string) (ProjectAttachmentView, io.ReadCloser, error) {
	item, err := service.repository.FindProjectAttachment(ctx, userID, projectID, storeKey)
	if err != nil {
		return ProjectAttachmentView{}, nil, err
	}
	reader, size, _, err := OpenFile(ctx, service.files, item.StoreKey)
	if err != nil {
		return ProjectAttachmentView{}, nil, fault.NotFound()
	}
	if size != item.FileSize {
		_ = reader.Close()
		return ProjectAttachmentView{}, nil, fault.Conflict("附件内容与登记信息不一致")
	}
	prefix, readErr := io.ReadAll(io.LimitReader(reader, 512))
	mediaType, valid := identifyProjectAttachment(item.FileName, prefix)
	if readErr != nil || !valid {
		_ = reader.Close()
		return ProjectAttachmentView{}, nil, fault.NotFound()
	}
	item.FileSize, item.ContentType = size, mediaType
	stream := &prefixedReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix), reader), Closer: reader}
	return ProjectAttachmentView{BusinessID: item.BusinessID, FileName: item.FileName, FileSize: item.FileSize, ContentType: item.ContentType, StoreKey: item.StoreKey}, stream, nil
}

type prefixedReadCloser struct {
	io.Reader
	io.Closer
}

// prepare 校验富文本文档版本、标题、封面、灵感图协议和附件字节，并构建精确搜索字段。
func (service *ProjectService) prepare(ctx context.Context, userID uuid.UUID, input ProjectSave) (model.Project, []uuid.UUID, []model.ProjectAttachment, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || utf8.RuneCountInString(title) > 120 {
		return model.Project{}, nil, nil, fault.InvalidField("title", "标题必填且最多 120 个字符")
	}
	if utf8.RuneCountInString(input.CoverEmoji) > 32 {
		return model.Project{}, nil, nil, fault.InvalidField("cover_emoji", "封面 emoji 最多 32 个字符")
	}
	if input.DocumentSchemaVersion != projectDocumentSchemaVersion {
		return model.Project{}, nil, nil, fault.InvalidField("document_schema_version", "不支持的拍摄策划文档版本")
	}
	if len(input.Document) == 0 || len(input.Document) > 8<<20 {
		return model.Project{}, nil, nil, fault.InvalidField("document", "正文无效或超过 8 MiB")
	}
	var root map[string]any
	if err := json.Unmarshal(input.Document, &root); err != nil || root["type"] != "doc" {
		return model.Project{}, nil, nil, fault.InvalidField("document", "正文必须是有效的 ProseMirror doc JSON")
	}
	coverFileName := ""
	if input.CoverImageKey == "" {
		if strings.TrimSpace(input.CoverEmoji) == "" {
			return model.Project{}, nil, nil, fault.InvalidField("cover_emoji", "请设置封面 emoji 或上传封面图片")
		}
	} else {
		if input.CoverEmoji != "" {
			return model.Project{}, nil, nil, fault.InvalidField("cover_image_key", "图片封面不能同时设置 emoji")
		}
		if err := service.validateFile(ctx, input.CoverImageKey, input.CoverFileName, true); err != nil {
			return model.Project{}, nil, nil, err
		}
		coverFileName = safeProjectFileName(input.CoverFileName)
	}

	text, inspirationSet, assetMap, err := service.inspectNodes(ctx, userID, root)
	if err != nil {
		return model.Project{}, nil, nil, err
	}
	searchText := normalizeProjectText(title + " " + text)
	grams := projectNGrams(searchText)
	attachments := make([]model.ProjectAttachment, 0, len(assetMap)+1)
	for _, item := range assetMap {
		attachments = append(attachments, item)
	}
	if input.CoverImageKey != "" {
		cover, err := service.describeFile(ctx, input.CoverImageKey, coverFileName, "cover")
		if err != nil {
			return model.Project{}, nil, nil, err
		}
		attachments = append(attachments, cover)
	}
	inspirations := make([]uuid.UUID, 0, len(inspirationSet))
	for id := range inspirationSet {
		inspirations = append(inspirations, id)
	}
	project := model.Project{Title: title, CoverEmoji: input.CoverEmoji, CoverImageKey: input.CoverImageKey, CoverFileName: coverFileName, DocumentSchemaVersion: projectDocumentSchemaVersion, Document: model.JSONB(input.Document), SearchText: searchText, SearchGrams: grams}
	return project, inspirations, attachments, nil
}

// inspectNodes 仅接受已知节点的纯 JSON 属性；提取文本供全文搜索并验证项目图片/附件引用。
func (service *ProjectService) inspectNodes(ctx context.Context, userID uuid.UUID, root map[string]any) (string, map[uuid.UUID]struct{}, map[string]model.ProjectAttachment, error) {
	var text strings.Builder
	inspirations := map[uuid.UUID]struct{}{}
	assets := map[string]model.ProjectAttachment{}
	var walk func(any) error
	walk = func(value any) error {
		switch node := value.(type) {
		case map[string]any:
			if typ, ok := node["type"].(string); ok {
				switch typ {
				case "text":
					if value, ok := node["text"].(string); ok {
						text.WriteString(value)
					}
				case "inspirationImage":
					attrs, _ := node["attrs"].(map[string]any)
					uri, _ := attrs["src"].(string)
					idText := strings.TrimPrefix(uri, "inspiration://")
					id, err := uuid.Parse(idText)
					if err != nil || !strings.HasPrefix(uri, "inspiration://") {
						return fault.InvalidField("document", "灵感库图片引用格式无效")
					}
					inspirations[id] = struct{}{}
				case "projectAttachment":
					attrs, _ := node["attrs"].(map[string]any)
					key, _ := attrs["storekey"].(string)
					name, _ := attrs["fileName"].(string)
					if strings.TrimSpace(key) == "" {
						return fault.InvalidField("document", "附件引用缺少 storekey")
					}
					item, err := service.describeFile(ctx, key, name, "content")
					if err != nil {
						return err
					}
					assets[key] = item
				case "image":
					return fault.InvalidField("document", "正文图片必须从灵感库中插入")
				}
				if typ == "paragraph" || typ == "heading" || typ == "listItem" || typ == "taskItem" || typ == "blockquote" || typ == "codeBlock" || typ == "tableCell" || typ == "tableHeader" {
					text.WriteByte(' ')
				}
			}
			if children, ok := node["content"]; ok {
				if err := walk(children); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range node {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return "", nil, nil, err
	}
	if len(inspirations) > 0 {
		ids := make([]uuid.UUID, 0, len(inspirations))
		for id := range inspirations {
			ids = append(ids, id)
		}
		if err := service.repository.ValidateProjectInspirations(ctx, userID, ids); err != nil {
			return "", nil, nil, err
		}
	}
	return text.String(), inspirations, assets, nil
}

// describeFile 从鉴权存储读取实际大小和内容类型，避免信任浏览器传入的 MIME 与文件长度。
func (service *ProjectService) describeFile(ctx context.Context, key, name, role string) (model.ProjectAttachment, error) {
	if service.files == nil {
		return model.ProjectAttachment{}, fault.Unavailable(nil)
	}
	if err := authorizeFileReference(ctx, service.files, key); err != nil {
		return model.ProjectAttachment{}, err
	}
	reader, size, _, err := OpenFile(ctx, service.files, key)
	if err != nil {
		return model.ProjectAttachment{}, fault.InvalidField("document", "附件不存在或无法读取")
	}
	prefix, readErr := io.ReadAll(io.LimitReader(reader, 512))
	_ = reader.Close()
	if readErr != nil {
		return model.ProjectAttachment{}, fault.InvalidField("document", "附件无法读取")
	}
	mediaType, ok := identifyProjectAttachment(name, prefix)
	if !ok || size < 1 || size > service.maxBytes {
		return model.ProjectAttachment{}, fault.InvalidField("document", "附件类型或大小不受支持")
	}
	return model.ProjectAttachment{BaseModel: model.BaseModel{BusinessID: uuid.New()}, StoreKey: key, FileName: safeProjectFileName(name), FileSize: size, ContentType: mediaType, AssetRole: role}, nil
}

// validateFile 校验封面图片真实字节，文件只在项目关联提交后进入项目附件清单。
func (service *ProjectService) validateFile(ctx context.Context, key, name string, imageOnly bool) error {
	item, err := service.describeFile(ctx, key, name, "cover")
	if err != nil {
		return err
	}
	if imageOnly && !strings.HasPrefix(item.ContentType, "image/") {
		return fault.InvalidField("cover_image_key", "封面必须是有效图片")
	}
	return nil
}

// identifyProjectAttachment 根据扩展名和文件头组合判断类型；Office 容器签名与 PDF/媒体签名都必须匹配。
func identifyProjectAttachment(filename string, prefix []byte) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return "application/pdf", bytes.HasPrefix(prefix, []byte("%PDF-"))
	case ".doc":
		return "application/msword", bytes.HasPrefix(prefix, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	case ".xls":
		return "application/vnd.ms-excel", bytes.HasPrefix(prefix, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document", bytes.HasPrefix(prefix, []byte("PK\x03\x04"))
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", bytes.HasPrefix(prefix, []byte("PK\x03\x04"))
	case ".gif":
		return "image/gif", bytes.HasPrefix(prefix, []byte("GIF87a")) || bytes.HasPrefix(prefix, []byte("GIF89a"))
	case ".jpg", ".jpeg":
		return "image/jpeg", bytes.HasPrefix(prefix, []byte{0xff, 0xd8, 0xff})
	case ".png":
		return "image/png", bytes.HasPrefix(prefix, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	case ".webp":
		return "image/webp", len(prefix) >= 12 && string(prefix[:4]) == "RIFF" && string(prefix[8:12]) == "WEBP"
	case ".mp4":
		return "video/mp4", len(prefix) >= 12 && string(prefix[4:8]) == "ftyp"
	case ".webm":
		return "video/webm", bytes.HasPrefix(prefix, []byte{0x1a, 0x45, 0xdf, 0xa3})
	case ".ogv":
		return "video/ogg", bytes.HasPrefix(prefix, []byte("OggS"))
	default:
		return "", false
	}
}

// safeProjectFileName 删除路径和控制字符，文件名只作展示且绝不作为存储路径。
func safeProjectFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	var out strings.Builder
	for _, r := range name {
		if !unicode.IsControl(r) {
			out.WriteRune(r)
		}
	}
	if out.Len() == 0 || utf8.RuneCountInString(out.String()) > 180 {
		return "attachment"
	}
	return out.String()
}

func projectView(item model.Project) ProjectView {
	return ProjectView{BusinessID: item.BusinessID, Title: item.Title, CoverEmoji: item.CoverEmoji, CoverImageKey: item.CoverImageKey, CoverFileName: item.CoverFileName, DocumentSchemaVersion: item.DocumentSchemaVersion, Document: json.RawMessage(item.Document), Revision: item.Revision, CreatedAt: item.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), UpdatedAt: item.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
}

func projectSummary(item model.Project) ProjectSummary {
	return ProjectSummary{BusinessID: item.BusinessID, Title: item.Title, CoverEmoji: item.CoverEmoji, CoverImageKey: item.CoverImageKey, CoverFileName: item.CoverFileName, Revision: item.Revision, CreatedAt: item.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), UpdatedAt: item.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
}

func normalizeProjectText(value string) string {
	value = strings.ToLower(norm.NFKC.String(value))
	return strings.Join(strings.Fields(value), " ")
}

func projectNGrams(value string) []string {
	runes := []rune(value)
	set := make(map[string]struct{}, len(runes)*2)
	for index, r := range runes {
		set[string(r)] = struct{}{}
		if index+1 < len(runes) {
			set[string(runes[index:index+2])] = struct{}{}
		}
	}
	grams := make([]string, 0, len(set))
	for gram := range set {
		grams = append(grams, gram)
	}
	return grams
}
