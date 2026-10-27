// daily-verify 仅在显式指定的独立测试库模拟每日worker入口，不对外提供手动AI业务接口。
package main

import (
	"context"
	"fmt"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
)

func main() {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		panic("需要显式独立测试DSN")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		panic(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	var name string
	if err = db.Raw("SELECT current_database()").Scan(&name).Error; err != nil || name != "photo_nini_test" {
		panic("仅允许独立photo_nini_test数据库")
	}
	repository := store.NewPostgresStore(db)
	repository.SetInspirationProcessingAlgorithm("yolox-nano-v1/color-v1")
	after, total := 0, 0
	for {
		next, count, err := repository.ScanInspirationProcessingPage(tenant.System(context.Background()), after, "yolox-nano-v1/color-v1")
		if err != nil {
			panic(err)
		}
		total += count
		after = next
		if count < 500 {
			break
		}
	}
	fmt.Printf("受控模拟每日扫描完成，未识别候选记录：%d\n", total)
}
