package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/model"
)

const feishuFeedbackImageMaxBytes = 10 * 1024 * 1024

// feishuFeedbackImageUploadSettingsReader 通过统一配置服务读取飞书 App ID、App Secret 和是否启用状态。
type feishuFeedbackImageUploadSettingsReader interface {
	FeishuFeedbackImageUploadSettings(context.Context) (string, string, bool, error)
}

// feedbackImageFileReader 提供只读 storekey 文件访问；调用方负责校验 key 类型和公开标记。
type feedbackImageFileReader interface {
	Open(string) (io.ReadCloser, int64, string, error)
}

// feishuFeedbackImageUploader 负责获取短期租户令牌、读取受限反馈图片并上传为飞书原生图片资源。
type feishuFeedbackImageUploader struct {
	settings feishuFeedbackImageUploadSettingsReader
	files    feedbackImageFileReader
	client   *http.Client

	mu            sync.Mutex
	cachedAppID   string
	cachedSecret  string
	cachedToken   string
	tokenValidTil time.Time
}

// feishuFeedbackImageReference 保存一段反馈 Markdown 中服务端生成图片引用的原始序号。
type feishuFeedbackImageReference struct {
	index    int
	storeKey string
}

// newFeishuFeedbackImageUploader 复用通知服务的限时、禁止重定向客户端处理飞书凭证和图片上传请求。
func newFeishuFeedbackImageUploader(settings feishuFeedbackImageUploadSettingsReader, client *http.Client) *feishuFeedbackImageUploader {
	return &feishuFeedbackImageUploader{settings: settings, client: client}
}

// SetFeedbackImageStore 注入只读图片存储；读取时仍会再次检查公共 key 标记和实际 MIME 类型。
func (service *NoticeService) SetFeedbackImageStore(files feedbackImageFileReader) {
	if service != nil && service.feishuFeedbackImage != nil {
		service.feishuFeedbackImage.files = files
	}
}

// prepare 将当前分段里的反馈图片上传到飞书，并把 Markdown 图片标记转为原生富文本 img 节点。
// 凭证两项都未设置时返回 handled=false，让调用方继续发送无需授权的公开图片链接。
func (uploader *feishuFeedbackImageUploader) prepare(ctx context.Context, config model.NoticeConfig, delivery model.NoticeDelivery) (string, []byte, bool, string) {
	var payload struct {
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
		return "", nil, false, ""
	}
	references, err := parseFeishuFeedbackImageReferences(payload.Markdown)
	if err != nil {
		return "", nil, false, "feishu_feedback_image_invalid"
	}
	if len(references) == 0 {
		return "", nil, false, ""
	}
	if uploader == nil || uploader.settings == nil || uploader.client == nil {
		return "", nil, false, ""
	}
	appID, appSecret, configured, err := uploader.settings.FeishuFeedbackImageUploadSettings(ctx)
	if err != nil {
		if errors.Is(err, ErrFeishuFeedbackImageConfigInvalid) {
			return "", nil, false, "feishu_image_config_invalid"
		}
		return "", nil, false, "feishu_image_config_unavailable"
	}
	if !configured {
		return "", nil, false, ""
	}
	if uploader.files == nil {
		return "", nil, false, "feishu_image_upload_unavailable"
	}
	token, err := uploader.tenantAccessToken(ctx, appID, appSecret)
	if err != nil {
		return "", nil, false, "feishu_image_auth_failed"
	}
	imageKeys := make(map[int]string, len(references))
	imageStoreKeys := make(map[int]string, len(references))
	for _, reference := range references {
		if reference.storeKey == "" {
			return "", nil, false, "feishu_feedback_image_invalid"
		}
		if _, exists := imageKeys[reference.index]; exists {
			if imageStoreKeys[reference.index] != reference.storeKey {
				return "", nil, false, "feishu_feedback_image_invalid"
			}
			continue
		}
		imageKey, uploadErr := uploader.uploadFeedbackImage(ctx, token, reference.storeKey)
		if uploadErr != nil {
			return "", nil, false, "feishu_image_upload_failed"
		}
		imageKeys[reference.index] = imageKey
		imageStoreKeys[reference.index] = reference.storeKey
	}
	body, err := feishuFeedbackPostBody(payload.Title, payload.Markdown, imageKeys)
	if err != nil {
		return "", nil, false, "feishu_feedback_image_invalid"
	}
	if config.SigningSecret != "" {
		body, err = signFeishuWebhookPayload(body, config.SigningSecret)
		if err != nil {
			return "", nil, false, "invalid_provider_request"
		}
	}
	return config.WebhookURL, body, true, ""
}

// tenantAccessToken 按应用凭证缓存短期租户令牌；令牌和应用密钥只保存在进程内存中。
func (uploader *feishuFeedbackImageUploader) tenantAccessToken(ctx context.Context, appID, appSecret string) (string, error) {
	uploader.mu.Lock()
	defer uploader.mu.Unlock()
	if uploader.cachedToken != "" && uploader.cachedAppID == appID && uploader.cachedSecret == appSecret && time.Now().Before(uploader.tokenValidTil) {
		return uploader.cachedToken, nil
	}
	requestBody, err := json.Marshal(map[string]string{"app_id": appID, "app_secret": appSecret})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := uploader.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(responseBody) > 1024*1024 || response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", errors.New("飞书应用凭证请求失败")
	}
	var result struct {
		Code   int    `json:"code"`
		Token  string `json:"tenant_access_token"`
		Expire int    `json:"expire"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil || result.Code != 0 || result.Token == "" || result.Expire < 1 {
		return "", errors.New("飞书应用凭证响应无效")
	}
	validFor := time.Duration(result.Expire) * time.Second * 9 / 10
	if validFor < time.Second {
		validFor = time.Second
	}
	uploader.cachedAppID = appID
	uploader.cachedSecret = appSecret
	uploader.cachedToken = result.Token
	uploader.tokenValidTil = time.Now().Add(validFor)
	return result.Token, nil
}

// uploadFeedbackImage 只读取带 public 标记且实际类型为受支持图片的反馈对象，并限制上传体积为飞书 10 MiB 上限。
func (uploader *feishuFeedbackImageUploader) uploadFeedbackImage(ctx context.Context, token, storeKey string) (string, error) {
	parsedKey, err := filestore.ParseKey(storeKey)
	if err != nil || !parsedKey.Public {
		return "", errors.New("反馈图片 key 无效")
	}
	file, size, contentType, err := uploader.files.Open(storeKey)
	if err != nil {
		return "", errors.New("反馈图片读取失败")
	}
	defer file.Close()
	if size < 1 || size > feishuFeedbackImageMaxBytes {
		return "", errors.New("反馈图片超出飞书上传限制")
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if !supportedFeishuFeedbackImageType(contentType) {
		return "", errors.New("反馈文件不是飞书支持的图片类型")
	}
	imageBytes, err := io.ReadAll(io.LimitReader(file, feishuFeedbackImageMaxBytes+1))
	if err != nil || len(imageBytes) == 0 || len(imageBytes) > feishuFeedbackImageMaxBytes {
		return "", errors.New("反馈图片读取失败")
	}

	var requestBody bytes.Buffer
	multipartWriter := multipart.NewWriter(&requestBody)
	if err := multipartWriter.WriteField("image_type", "message"); err != nil {
		return "", err
	}
	imagePart, err := multipartWriter.CreateFormFile("image", "feedback."+parsedKey.Extension)
	if err != nil {
		return "", err
	}
	if _, err := imagePart.Write(imageBytes); err != nil {
		return "", err
	}
	if err := multipartWriter.Close(); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.feishu.cn/open-apis/im/v1/images", &requestBody)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	response, err := uploader.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	responseBytes, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(responseBytes) > 1024*1024 {
		return "", errors.New("飞书图片上传响应无效")
	}
	if response.StatusCode == http.StatusUnauthorized {
		uploader.invalidateToken(token)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", errors.New("飞书图片上传请求失败")
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			ImageKey string `json:"image_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseBytes, &result); err != nil || result.Code != 0 || result.Data.ImageKey == "" {
		return "", errors.New("飞书图片上传响应无效")
	}
	return result.Data.ImageKey, nil
}

// invalidateToken 清除本次上传被拒绝的缓存令牌，后续重试会重新向飞书申请。
func (uploader *feishuFeedbackImageUploader) invalidateToken(token string) {
	uploader.mu.Lock()
	defer uploader.mu.Unlock()
	if uploader.cachedToken == token {
		uploader.cachedAppID = ""
		uploader.cachedSecret = ""
		uploader.cachedToken = ""
		uploader.tokenValidTil = time.Time{}
	}
}

// supportedFeishuFeedbackImageType 将未知二进制和可主动执行的 SVG 排除在机器人图片上传之外。
func supportedFeishuFeedbackImageType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "image/bmp", "image/x-icon", "image/vnd.microsoft.icon", "image/tiff", "image/heic":
		return true
	default:
		return false
	}
}

// parseFeishuFeedbackImageReferences 只识别服务端生成、未转义的反馈图片标记，忽略用户正文中的相似字符串。
func parseFeishuFeedbackImageReferences(markdown string) ([]feishuFeedbackImageReference, error) {
	references := make([]feishuFeedbackImageReference, 0)
	for offset := 0; offset < len(markdown); {
		if markdown[offset] == '\\' {
			_, size := utf8.DecodeRuneInString(markdown[offset:])
			if size < 1 {
				return nil, errors.New("反馈 Markdown 编码无效")
			}
			offset += size
			if offset < len(markdown) {
				_, escapedSize := utf8.DecodeRuneInString(markdown[offset:])
				offset += escapedSize
			}
			continue
		}
		if !strings.HasPrefix(markdown[offset:], "![反馈图片") {
			_, size := utf8.DecodeRuneInString(markdown[offset:])
			if size < 1 {
				return nil, errors.New("反馈 Markdown 编码无效")
			}
			offset += size
			continue
		}
		start := offset + len("![反馈图片")
		labelEnd := strings.Index(markdown[start:], "](")
		if labelEnd < 1 {
			return nil, errors.New("反馈图片引用无效")
		}
		index, err := strconv.Atoi(markdown[start : start+labelEnd])
		if err != nil || index < 1 {
			return nil, errors.New("反馈图片序号无效")
		}
		urlStart := start + labelEnd + 2
		urlEnd := strings.IndexByte(markdown[urlStart:], ')')
		if urlEnd < 1 {
			return nil, errors.New("反馈图片链接无效")
		}
		imageURL := markdown[urlStart : urlStart+urlEnd]
		parsedURL, err := url.Parse(imageURL)
		if err != nil || !strings.EqualFold(parsedURL.Scheme, "https") || parsedURL.Hostname() == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
			return nil, errors.New("反馈图片链接无效")
		}
		pathSegments := strings.Split(strings.Trim(parsedURL.EscapedPath(), "/"), "/")
		if len(pathSegments) == 0 {
			return nil, errors.New("反馈图片路径无效")
		}
		storeKey, err := url.PathUnescape(pathSegments[len(pathSegments)-1])
		if err != nil {
			return nil, errors.New("反馈图片路径无效")
		}
		parsedKey, err := filestore.ParseKey(storeKey)
		if err != nil || !parsedKey.Public || !strings.HasSuffix(parsedURL.Path, "/api/v1/public-feedback-images/"+storeKey) {
			return nil, errors.New("反馈图片 key 无效")
		}
		references = append(references, feishuFeedbackImageReference{index: index, storeKey: storeKey})
		offset = urlStart + urlEnd + 1
	}
	return references, nil
}

// feishuFeedbackPostBody 生成飞书 post 富文本段落，把图片引用换成 img 节点并将反馈文字作为纯文本发送。
func feishuFeedbackPostBody(title, markdown string, imageKeys map[int]string) ([]byte, error) {
	paragraphs := make([][]map[string]string, 0, 8)
	paragraph := make([]map[string]string, 0, 4)
	var text strings.Builder
	flushText := func() {
		if text.Len() == 0 {
			return
		}
		paragraph = append(paragraph, map[string]string{"tag": "text", "text": text.String()})
		text.Reset()
	}
	flushParagraph := func() {
		flushText()
		if len(paragraph) > 0 {
			paragraphs = append(paragraphs, paragraph)
			paragraph = make([]map[string]string, 0, 4)
		}
	}
	for offset := 0; offset < len(markdown); {
		if strings.HasPrefix(markdown[offset:], "![反馈图片") {
			flushParagraph()
			index, _, nextOffset, err := parseFeedbackImageReferenceAt(markdown, offset)
			if err != nil || imageKeys[index] == "" {
				return nil, errors.New("反馈图片关联信息无效")
			}
			paragraphs = append(paragraphs, []map[string]string{{"tag": "img", "image_key": imageKeys[index]}})
			offset = nextOffset
			continue
		}
		if strings.HasPrefix(markdown[offset:], "[查看反馈图片") {
			flushText()
			index, imageURL, nextOffset, err := parseFeedbackLinkReferenceAt(markdown, offset)
			if err == nil && imageKeys[index] != "" {
				paragraph = append(paragraph, map[string]string{"tag": "a", "text": fmt.Sprintf("查看反馈图片%d", index), "href": imageURL})
				offset = nextOffset
				continue
			}
		}
		if markdown[offset] == '\n' {
			flushParagraph()
			offset++
			continue
		}
		if markdown[offset] == '\\' && offset+1 < len(markdown) {
			offset++
			runeValue, size := utf8.DecodeRuneInString(markdown[offset:])
			if size < 1 {
				return nil, errors.New("反馈 Markdown 编码无效")
			}
			text.WriteRune(runeValue)
			offset += size
			continue
		}
		runeValue, size := utf8.DecodeRuneInString(markdown[offset:])
		if size < 1 {
			return nil, errors.New("反馈 Markdown 编码无效")
		}
		text.WriteRune(runeValue)
		offset += size
	}
	flushParagraph()
	if len(paragraphs) == 0 {
		paragraphs = append(paragraphs, []map[string]string{{"tag": "text", "text": "收到新的用户反馈"}})
	}
	requestPayload := map[string]any{
		"msg_type": "post",
		"content": map[string]any{
			"post": map[string]any{
				"zh_cn": map[string]any{"title": title, "content": paragraphs},
			},
		},
	}
	return json.Marshal(requestPayload)
}

// parseFeedbackImageReferenceAt 解析一条服务端生成的 Markdown 图片引用及其结束位置。
func parseFeedbackImageReferenceAt(markdown string, offset int) (int, string, int, error) {
	start := offset + len("![反馈图片")
	labelEnd := strings.Index(markdown[start:], "](")
	if labelEnd < 1 {
		return 0, "", 0, errors.New("反馈图片引用无效")
	}
	index, err := strconv.Atoi(markdown[start : start+labelEnd])
	if err != nil || index < 1 {
		return 0, "", 0, errors.New("反馈图片序号无效")
	}
	urlStart := start + labelEnd + 2
	urlEnd := strings.IndexByte(markdown[urlStart:], ')')
	if urlEnd < 1 {
		return 0, "", 0, errors.New("反馈图片链接无效")
	}
	return index, markdown[urlStart : urlStart+urlEnd], urlStart + urlEnd + 1, nil
}

// parseFeedbackLinkReferenceAt 解析配对的“查看反馈图片”链接，仅将其作为 HTTPS 超链接放入机器人富文本。
func parseFeedbackLinkReferenceAt(markdown string, offset int) (int, string, int, error) {
	start := offset + len("[查看反馈图片")
	labelEnd := strings.Index(markdown[start:], "](")
	if labelEnd < 1 {
		return 0, "", 0, errors.New("反馈图片链接无效")
	}
	index, err := strconv.Atoi(markdown[start : start+labelEnd])
	if err != nil || index < 1 {
		return 0, "", 0, errors.New("反馈图片序号无效")
	}
	urlStart := start + labelEnd + 2
	urlEnd := strings.IndexByte(markdown[urlStart:], ')')
	if urlEnd < 1 {
		return 0, "", 0, errors.New("反馈图片链接无效")
	}
	imageURL := markdown[urlStart : urlStart+urlEnd]
	parsedURL, err := url.Parse(imageURL)
	if err != nil || !strings.EqualFold(parsedURL.Scheme, "https") || parsedURL.Hostname() == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return 0, "", 0, errors.New("反馈图片链接无效")
	}
	return index, imageURL, urlStart + urlEnd + 1, nil
}

// signFeishuWebhookPayload 按飞书自定义机器人约定对最终 JSON 载荷签名。
func signFeishuWebhookPayload(body []byte, secret string) ([]byte, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["timestamp"] = timestamp
	payload["sign"] = signature
	return json.Marshal(payload)
}
