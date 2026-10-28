package service

import (
	"encoding/json"
	"errors"
)

// PortfolioMetadataForPublic 返回作品作者明确允许公开的图片元数据快照。
// 原始 EXIF 可包含 GPS、设备序列号及厂商信息；调用方必须只在作品集公开分享时使用。
func PortfolioMetadataForPublic(raw json.RawMessage) (json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("图片属性必须是 JSON 对象")
	}
	return append(json.RawMessage(nil), raw...), nil
}
