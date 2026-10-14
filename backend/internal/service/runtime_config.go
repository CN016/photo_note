package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/photo-nini/backend/internal/config"
)

// RuntimeConfigReader 是唯一配置表读取入口；业务服务不得自行查询 app_configs。
type RuntimeConfigReader interface {
	ReadConfigValues(context.Context, []string) (map[string]string, error)
}

// RuntimeConfigService 将配置表中的运行设置解析为强类型值，供 HTTP 和认证组件统一使用。
type RuntimeConfigService struct {
	reader RuntimeConfigReader
}

// NewRuntimeConfigService 注入配置 repository，确保所有运行参数通过同一配置服务读取。
func NewRuntimeConfigService(reader RuntimeConfigReader) *RuntimeConfigService {
	return &RuntimeConfigService{reader: reader}
}

// Load 从 app_configs 读取服务设置并校验格式；缺项或非法值会阻止服务在不确定配置下启动。
func (service *RuntimeConfigService) Load(ctx context.Context, base config.Config) (config.Config, error) {
	keys := []string{
		"app.mode",
		"http.listen_addr",
		"http.max_json_body_bytes",
		"http.read_header_timeout",
		"http.read_timeout",
		"http.write_timeout",
		"http.idle_timeout",
		"auth.access_token_ttl",
		"auth.refresh_token_ttl",
		"files.local_root",
		"files.max_upload_bytes",
		"files.active_store_type",
	}
	values, err := service.reader.ReadConfigValues(ctx, keys)
	if err != nil {
		return config.Config{}, fmt.Errorf("读取服务运行配置失败: %w", err)
	}
	for _, key := range keys {
		if strings.TrimSpace(values[key]) == "" {
			return config.Config{}, fmt.Errorf("配置表缺少必需配置项 %s", key)
		}
	}

	base.Mode = values["app.mode"]
	if base.Mode != "release" && base.Mode != "debug" && base.Mode != "test" {
		return config.Config{}, errors.New("配置项 app.mode 只允许 release、debug 或 test")
	}
	base.HTTPAddr = values["http.listen_addr"]
	if base.HTTPAddr == "" {
		return config.Config{}, errors.New("配置项 http.listen_addr 不能为空")
	}
	base.MaxJSONBodyBytes, err = strconv.ParseInt(values["http.max_json_body_bytes"], 10, 64)
	if err != nil || base.MaxJSONBodyBytes < 1 {
		return config.Config{}, errors.New("配置项 http.max_json_body_bytes 必须是正整数")
	}
	base.LocalStorageRoot = strings.TrimSpace(values["files.local_root"])
	if base.LocalStorageRoot == "" {
		return config.Config{}, errors.New("配置项 files.local_root 不能为空")
	}
	base.MaxUploadBytes, err = strconv.ParseInt(values["files.max_upload_bytes"], 10, 64)
	if err != nil || base.MaxUploadBytes < 1 || base.MaxUploadBytes > math.MaxInt64-(1<<20) {
		return config.Config{}, errors.New("配置项 files.max_upload_bytes 必须是可安全处理的正整数")
	}
	base.ActiveStoreType = strings.TrimSpace(values["files.active_store_type"])
	if !supportedFileStoreType(base.ActiveStoreType) {
		return config.Config{}, errors.New("配置项 files.active_store_type 只允许 local 或 oss")
	}
	if base.HTTPReadHeaderTimeout, err = parseRuntimeDuration("http.read_header_timeout", values["http.read_header_timeout"]); err != nil {
		return config.Config{}, err
	}
	if base.HTTPReadTimeout, err = parseRuntimeDuration("http.read_timeout", values["http.read_timeout"]); err != nil {
		return config.Config{}, err
	}
	if base.HTTPWriteTimeout, err = parseRuntimeDuration("http.write_timeout", values["http.write_timeout"]); err != nil {
		return config.Config{}, err
	}
	if base.HTTPIdleTimeout, err = parseRuntimeDuration("http.idle_timeout", values["http.idle_timeout"]); err != nil {
		return config.Config{}, err
	}
	if base.AccessTokenTTL, err = parseRuntimeDuration("auth.access_token_ttl", values["auth.access_token_ttl"]); err != nil {
		return config.Config{}, err
	}
	if base.RefreshTokenTTL, err = parseRuntimeDuration("auth.refresh_token_ttl", values["auth.refresh_token_ttl"]); err != nil {
		return config.Config{}, err
	}
	if base.RefreshTokenTTL <= base.AccessTokenTTL {
		return config.Config{}, errors.New("配置项 auth.refresh_token_ttl 必须长于 auth.access_token_ttl")
	}
	return base, nil
}

// CurrentFileStoreType 在每次上传时重新读取当前存储类型，使配置表切换能影响后续写入而无需重启服务。
func (service *RuntimeConfigService) CurrentFileStoreType(ctx context.Context) (string, error) {
	values, err := service.reader.ReadConfigValues(ctx, []string{"files.active_store_type"})
	if err != nil {
		return "", fmt.Errorf("读取当前文件存储类型失败: %w", err)
	}
	storeType := strings.TrimSpace(values["files.active_store_type"])
	if !supportedFileStoreType(storeType) {
		return "", errors.New("配置项 files.active_store_type 只允许 local 或 oss")
	}
	return storeType, nil
}

// supportedFileStoreType 限定配置表中的存储类型为接口 key 已定义的值。
func supportedFileStoreType(storeType string) bool {
	return storeType == "local" || storeType == "oss"
}

// parseRuntimeDuration 校验表内时长配置，错误只指出配置键，不输出配置值。
func parseRuntimeDuration(key, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second {
		return 0, fmt.Errorf("配置项 %s 必须是至少一秒的 Go duration，例如 2h 或 720h", key)
	}
	return duration, nil
}
