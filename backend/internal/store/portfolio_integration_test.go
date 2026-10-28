package store

import (
	"context"
	"errors"
	"gorm.io/gorm/clause"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestPortfolioPostgresScope 在独立 PostgreSQL 验证作品集、标签、分享和文件清理的范围与事务。
func TestPortfolioPostgresScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	if dsn == "" {
		t.Skip("未配置独立 PostgreSQL 测试库")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	repo := NewPostgresStore(db)
	user, other := uuid.New(), uuid.New()
	personal := tenant.Personal(context.Background(), user)
	orgA := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 701, UserID: user})
	orgB := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 702, UserID: user})
	otherUser := tenant.WithScope(context.Background(), tenant.Scope{OrgID: 701, UserID: other})
	item := model.Portfolio{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Title: "test", TagIDs: pq.StringArray{}}
	if err := repo.CreatePortfolio(orgA, &item); err != nil {
		t.Fatal(err)
	}
	// 同一用户的个人空间、两个组织以及同组织另一创建者各有独立作品集。
	for _, sample := range []struct {
		ctx   context.Context
		owner uuid.UUID
	}{{personal, user}, {orgB, user}, {otherUser, other}} {
		own := model.Portfolio{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: sample.owner, Title: "isolated", TagIDs: pq.StringArray{}}
		if err := repo.CreatePortfolio(sample.ctx, &own); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.FindPortfolio(sample.ctx, sample.owner, own.BusinessID); err != nil || got.BusinessID != own.BusinessID {
			t.Fatalf("对应空间应能读取自身作品集: %v", err)
		}
	}
	for _, ctx := range []context.Context{personal, orgB, otherUser, context.Background()} {
		if _, err := repo.FindPortfolio(ctx, user, item.BusinessID); err == nil {
			t.Fatal("跨用户或跨空间可读取作品集")
		}
	}
	tag := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Content: "portfolio-test"}
	if err := repo.CreateTag(orgA, &tag); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdatePortfolio(orgA, user, item.BusinessID, "updated", "", []uuid.UUID{tag.BusinessID}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdatePortfolio(orgB, user, item.BusinessID, "bad", "", []uuid.UUID{tag.BusinessID}, nil); err == nil {
		t.Fatal("跨空间更新应拒绝")
	}
	// 同一用户在另一组织的策划不能关联到当前作品集；同组织另一用户也不能写入。
	project := model.Project{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Title: "other organization", CoverEmoji: "cover", Document: model.JSONB(`{"type":"doc","content":[{"type":"paragraph"}]}`)}
	if err := db.WithContext(orgB).Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdatePortfolio(orgA, user, item.BusinessID, "bad", "", nil, []uuid.UUID{project.BusinessID}); err == nil {
		t.Fatal("跨组织策划关联应拒绝")
	}
	if _, err := repo.UpdatePortfolio(otherUser, other, item.BusinessID, "bad", "", nil, nil); err == nil {
		t.Fatal("同组织其他用户不应修改作品集")
	}
	ownedProject := model.Project{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Title: "owned project", CoverEmoji: "cover", Document: model.JSONB(`{"type":"doc","content":[{"type":"paragraph"}]}`)}
	if err := db.WithContext(orgA).Create(&ownedProject).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdatePortfolio(orgA, user, item.BusinessID, "updated", "", []uuid.UUID{tag.BusinessID}, []uuid.UUID{ownedProject.BusinessID}); err != nil {
		t.Fatal(err)
	}
	if links, err := repo.ListPortfolioProjects(orgA, user, item.BusinessID); err != nil || len(links) != 1 {
		t.Fatalf("策划关联失败: %v", err)
	}
	if err := repo.DeleteProject(orgA, user, ownedProject.BusinessID); err != nil {
		t.Fatal(err)
	}
	if links, err := repo.ListPortfolioProjects(orgA, user, item.BusinessID); err != nil || len(links) != 0 {
		t.Fatalf("删除策划后关联未清除: %v", err)
	}
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := repo.SetPortfolioShareHash(orgA, user, item.BusinessID, &hash); err != nil {
		t.Fatal(err)
	}
	shared, err := repo.FindPortfolioByShareHash(context.Background(), hash)
	if err != nil || shared.OrgID != 701 || shared.UserID != user {
		t.Fatalf("分享范围错误: %v", err)
	}
	if err := repo.SetPortfolioShareHash(orgA, user, item.BusinessID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindPortfolioByShareHash(context.Background(), hash); err == nil {
		t.Fatal("撤销分享后仍可读取")
	}
	// 模拟标签删除持锁期间并发创建：创建必须等待并在标签删除后拒绝。
	concurrentTag := model.Tag{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Content: "concurrent-" + uuid.NewString()[:8]}
	if err := repo.CreateTag(orgA, &concurrentTag); err != nil {
		t.Fatal(err)
	}
	tx := db.WithContext(orgA).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	var locked model.Tag
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND business_id = ?", user, concurrentTag.BusinessID).First(&locked).Error; err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	created := make(chan error, 1)
	go func() {
		candidate := model.Portfolio{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, Title: "concurrent", TagIDs: pq.StringArray{concurrentTag.BusinessID.String()}}
		created <- repo.CreatePortfolio(orgA, &candidate)
	}()
	select {
	case err := <-created:
		tx.Rollback()
		t.Fatalf("创建未等待标签锁: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := tx.Delete(&locked).Error; err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-created:
		if err == nil {
			t.Fatal("标签删除后不应创建带失效标签的作品集")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("创建等待锁超时")
	}
	photo := model.PortfolioPhoto{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: user, PortfolioID: item.BusinessID, ImageKey: "public-key", TagIDs: pq.StringArray{}, ImageMetadata: model.JSONB(`{}`)}
	if err := repo.CreatePortfolioPhoto(orgA, &photo); err != nil {
		t.Fatal(err)
	}
	covers, err := repo.ListPortfolioCovers(orgA, user, []uuid.UUID{item.BusinessID})
	if err != nil || len(covers) != 1 || covers[0].ImageKey != "public-key" {
		t.Fatalf("首图读取失败: %#v %v", covers, err)
	}
	covers, err = repo.ListPortfolioCovers(orgB, user, []uuid.UUID{item.BusinessID})
	if err != nil || len(covers) != 0 {
		t.Fatalf("跨空间首图泄露: %#v %v", covers, err)
	}
	if _, err := repo.UpdatePortfolioPhoto(orgA, user, item.BusinessID, photo.BusinessID, "photo", []uuid.UUID{tag.BusinessID}, 4); err != nil {
		t.Fatal(err)
	}
	// 删除标签时，作品集及照片中的 UUID 引用必须同步清除。
	if err := repo.DeleteTagAndCleanInspirations(orgA, user, tag.BusinessID); err != nil {
		t.Fatal(err)
	}
	cleaned, err := repo.FindPortfolio(orgA, user, item.BusinessID)
	if err != nil || len(cleaned.TagIDs) != 0 {
		t.Fatalf("作品集标签未清除: %v", err)
	}
	cleanedPhotos, err := repo.ListPortfolioPhotos(orgA, user, item.BusinessID)
	if err != nil || len(cleanedPhotos) != 1 || len(cleanedPhotos[0].TagIDs) != 0 {
		t.Fatalf("照片标签未清除: %v", err)
	}
	if _, err := repo.ListPortfolioPhotos(orgB, user, item.BusinessID); err == nil {
		t.Fatal("跨空间读取照片应拒绝")
	}
	fail := errors.New("cleanup failure")
	if err := repo.DeletePortfolio(orgA, user, item.BusinessID, func(string) error { return fail }); !errors.Is(err, fail) {
		t.Fatalf("清理失败应中止删除: %v", err)
	}
	if _, err := repo.FindPortfolio(orgA, user, item.BusinessID); err != nil {
		t.Fatal("失败后作品集应保留", err)
	}
	if err := repo.DeletePortfolio(orgA, user, item.BusinessID, func(key string) error {
		if key != "public-key" {
			t.Fatal("文件键错误")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindPortfolio(orgA, user, item.BusinessID); err == nil {
		t.Fatal("删除后作品集仍存在")
	}
}
