package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemoryKiB uint32 = 64 * 1024
	argonTime      uint32 = 3
	argonThreads   uint8  = 4
	argonKeyBytes  uint32 = 32
	argonSaltBytes        = 16
)

// PasswordManager 封装 Argon2id 密码哈希，并持有随机虚拟哈希以平衡不存在用户的登录耗时。
type PasswordManager struct {
	dummyHash string
}

// NewPasswordManager 生成仅用于时序均衡的虚拟密码哈希；虚拟密码本身不会保留或写入日志。
func NewPasswordManager() (*PasswordManager, error) {
	dummyPassword := make([]byte, 32)
	if _, err := rand.Read(dummyPassword); err != nil {
		return nil, fmt.Errorf("生成虚拟密码失败: %w", err)
	}
	hash, err := hashPassword(dummyPassword)
	for index := range dummyPassword {
		dummyPassword[index] = 0
	}
	if err != nil {
		return nil, err
	}
	return &PasswordManager{dummyHash: hash}, nil
}

// Hash 使用独立密码学随机盐和 Argon2id 计算不可逆哈希，数据库只保存编码后的参数、盐和摘要。
func (manager *PasswordManager) Hash(password string) (string, error) {
	return hashPassword([]byte(password))
}

// Verify 对已保存哈希进行常量时间摘要比较，不匹配时返回 false 而不泄露更多信息。
func (manager *PasswordManager) Verify(password, encoded string) (bool, error) {
	return verifyPassword([]byte(password), encoded)
}

// VerifyDummy 对不存在的手机号执行同成本哈希计算，降低通过响应时间枚举账户的风险。
func (manager *PasswordManager) VerifyDummy(password string) {
	_, _ = verifyPassword([]byte(password), manager.dummyHash)
}

// hashPassword 为单个密码生成盐并采用 RFC 9106 推荐的低内存 Argon2id 参数计算摘要。
func hashPassword(password []byte) (string, error) {
	salt := make([]byte, argonSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐失败: %w", err)
	}
	key := argon2.IDKey(password, salt, argonTime, argonMemoryKiB, argonThreads, argonKeyBytes)
	encode := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		encode.EncodeToString(salt), encode.EncodeToString(key)), nil
}

// verifyPassword 解析哈希内的成本参数并设置上限，避免损坏数据库内容触发无限内存或 CPU 消耗。
func verifyPassword(password []byte, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("密码哈希格式无效")
	}
	version, err := strconv.ParseUint(strings.TrimPrefix(parts[2], "v="), 10, 32)
	if err != nil || version != uint64(argon2.Version) {
		return false, fmt.Errorf("密码哈希版本不受支持")
	}
	var memory, iterations uint32
	var threads uint8
	for _, parameter := range strings.Split(parts[3], ",") {
		keyValue := strings.SplitN(parameter, "=", 2)
		if len(keyValue) != 2 {
			return false, fmt.Errorf("密码哈希参数无效")
		}
		switch keyValue[0] {
		case "m":
			parsed, parseErr := strconv.ParseUint(keyValue[1], 10, 32)
			if parseErr != nil {
				return false, fmt.Errorf("密码哈希内存参数无效")
			}
			memory = uint32(parsed)
		case "t":
			parsed, parseErr := strconv.ParseUint(keyValue[1], 10, 32)
			if parseErr != nil {
				return false, fmt.Errorf("密码哈希迭代参数无效")
			}
			iterations = uint32(parsed)
		case "p":
			parsed, parseErr := strconv.ParseUint(keyValue[1], 10, 8)
			if parseErr != nil {
				return false, fmt.Errorf("密码哈希并行参数无效")
			}
			threads = uint8(parsed)
		default:
			return false, fmt.Errorf("密码哈希包含未知参数")
		}
	}
	if memory < 8*uint32(threads) || memory > 256*1024 || iterations == 0 || iterations > 10 || threads == 0 || threads > 32 {
		return false, fmt.Errorf("密码哈希成本参数超出允许范围")
	}
	decode := base64.RawStdEncoding
	salt, err := decode.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, fmt.Errorf("密码哈希盐值无效")
	}
	expected, err := decode.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false, fmt.Errorf("密码哈希摘要无效")
	}
	actual := argon2.IDKey(password, salt, iterations, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
