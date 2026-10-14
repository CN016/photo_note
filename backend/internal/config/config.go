package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config 汇总数据库/Redis连接参数以及从配置表加载的 HTTP 和认证运行参数。
type Config struct {
	Mode                  string
	HTTPAddr              string
	DBHost                string
	DBPort                int
	DBName                string
	DBUser                string
	DBPassword            string
	DBSSLMode             string
	DBMaxOpenConns        int
	DBMaxIdleConns        int
	DBConnMaxLifetime     time.Duration
	RedisAddr             string
	RedisPassword         string
	RedisDB               int
	AccessTokenTTL        time.Duration
	RefreshTokenTTL       time.Duration
	MaxJSONBodyBytes      int64
	MaxUploadBytes        int64
	LocalStorageRoot      string
	ActiveStoreType       string
	HTTPReadHeaderTimeout time.Duration
	HTTPReadTimeout       time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration
}

// Load 只从环境变量读取 PostgreSQL/Redis 连接参数；其他服务配置由配置表服务补齐。
func Load() (Config, error) {
	cfg := Config{
		DBHost:            envString("DB_HOST", "127.0.0.1"),
		DBName:            firstEnv("DB_NAME", "POSTGRES_DB"),
		DBUser:            firstEnv("DB_USER", "POSTGRES_USER"),
		DBPassword:        firstEnv("DB_PASSWORD", "POSTGRES_PASSWORD"),
		DBSSLMode:         envString("DB_SSLMODE", "disable"),
		RedisAddr:         envString("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:     envString("REDIS_PASSWORD", ""),
		DBMaxOpenConns:    25,
		DBMaxIdleConns:    5,
		DBConnMaxLifetime: 30 * time.Minute,
	}

	var err error
	if cfg.DBPort, err = envInt("DB_PORT", 5432); err != nil {
		return Config{}, err
	}
	if cfg.RedisDB, err = envInt("REDIS_DB", 0); err != nil {
		return Config{}, err
	}
	if cfg.DBMaxOpenConns, err = envInt("DB_MAX_OPEN_CONNS", cfg.DBMaxOpenConns); err != nil {
		return Config{}, err
	}
	if cfg.DBMaxIdleConns, err = envInt("DB_MAX_IDLE_CONNS", cfg.DBMaxIdleConns); err != nil {
		return Config{}, err
	}
	if cfg.DBConnMaxLifetime, err = envDuration("DB_CONN_MAX_LIFETIME", cfg.DBConnMaxLifetime); err != nil {
		return Config{}, err
	}
	if cfg.DBHost == "" || cfg.DBName == "" || cfg.DBUser == "" || cfg.DBPassword == "" || cfg.RedisAddr == "" {
		return Config{}, fmt.Errorf("必须设置 POSTGRES_DB、POSTGRES_USER 和 POSTGRES_PASSWORD，或提供对应 DB_* 变量")
	}
	if cfg.DBPort < 1 || cfg.DBPort > 65535 || cfg.RedisDB < 0 {
		return Config{}, fmt.Errorf("数据库端口或 Redis 数据库编号无效")
	}
	if cfg.DBMaxOpenConns < 1 || cfg.DBMaxIdleConns < 0 || cfg.DBMaxIdleConns > cfg.DBMaxOpenConns {
		return Config{}, fmt.Errorf("PostgreSQL 连接池参数无效")
	}
	return cfg, nil
}

// envString 读取单个配置项，并在未设置或为空时采用缺省值。
func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// firstEnv 按优先级读取数据库专属变量和 PostgreSQL 容器标准变量。
func firstEnv(primary, fallback string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	return os.Getenv(fallback)
}

// envInt 解析正整数配置，并在格式错误时指出变量名但不回显变量值。
func envInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("环境变量 %s 必须是整数", key)
	}
	return parsed, nil
}

// envDuration 解析 PostgreSQL 连接池生命周期，单位由 Go duration 后缀明确表达。
func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("环境变量 %s 必须使用如 30m 的 Go duration 格式", key)
	}
	return parsed, nil
}
