package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ClaimFeedbackNotificationJob 使用行锁领取一项通知任务，并在同一事务中读取对应反馈正文。
func (store *PostgresStore) ClaimFeedbackNotificationJob(ctx context.Context) (model.FeedbackNotificationJob, model.Feedback, bool, error) {
	var job model.FeedbackNotificationJob
	var feedback model.Feedback
	claimed := false
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("(job_status IN ('pending', 'retry') AND next_attempt_at <= ?) OR (job_status = 'processing' AND updated_at <= ?)", now, now.Add(-2*time.Minute)).
			Order("next_attempt_at ASC, created_at ASC").Limit(1).Find(&job)
		if result.Error != nil {
			return persistenceError(result.Error)
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if job.AttemptCount >= 5 {
			if err := tx.Model(&job).Updates(map[string]any{
				"job_status": "failed", "last_error_code": "attempts_exhausted",
				"completed_at": now, "updated_at": now,
			}).Error; err != nil {
				return persistenceError(err)
			}
			return nil
		}
		job.Status = "processing"
		job.AttemptCount++
		job.LastAttemptAt = &now
		job.UpdatedAt = now
		if err := tx.Model(&job).Updates(map[string]any{
			"job_status": job.Status, "attempt_count": job.AttemptCount,
			"last_attempt_at": now, "updated_at": now,
		}).Error; err != nil {
			return persistenceError(err)
		}
		if err := tx.Where("business_id = ?", job.FeedbackBusinessID).First(&feedback).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if updateErr := tx.Model(&job).Updates(map[string]any{
					"job_status": "failed", "last_error_code": "feedback_missing",
					"completed_at": now, "updated_at": now,
				}).Error; updateErr != nil {
					return persistenceError(updateErr)
				}
				return nil
			}
			return persistenceError(err)
		}
		claimed = true
		return nil
	})
	return job, feedback, claimed, err
}

// CompleteFeedbackNotificationJob 记录成功、跳过、永久失败或可重试结果；暂时性错误最多执行五次。
func (store *PostgresStore) CompleteFeedbackNotificationJob(ctx context.Context, jobID uuid.UUID, attemptCount int, outcome, errorCode string) error {
	now := time.Now().UTC()
	updates := map[string]any{"updated_at": now, "last_error_code": errorCode}
	switch outcome {
	case "succeeded", "skipped", "failed":
		updates["job_status"] = outcome
		updates["completed_at"] = now
	case "retry":
		if attemptCount >= 5 {
			updates["job_status"] = "failed"
			updates["completed_at"] = now
		} else {
			delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
			index := attemptCount - 1
			if index < 0 {
				index = 0
			}
			updates["job_status"] = "retry"
			updates["next_attempt_at"] = now.Add(delays[index])
		}
	default:
		return errors.New("反馈通知任务结果状态无效")
	}
	result := store.db.WithContext(ctx).Model(&model.FeedbackNotificationJob{}).
		Where("business_id = ? AND job_status = 'processing'", jobID).Updates(updates)
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

// FindFeedbackReporter 只读取反馈人的昵称和手机号，供管理员通知识别反馈来源。
func (store *PostgresStore) FindFeedbackReporter(ctx context.Context, userID uuid.UUID) (string, string, error) {
	var user struct {
		Nickname string `gorm:"column:nickname"`
		Phone    string `gorm:"column:phone"`
	}
	err := store.db.WithContext(ctx).Model(&model.User{}).
		Select("nickname", "phone").Where("business_id = ?", userID).First(&user).Error
	if err != nil {
		return "", "", persistenceError(err)
	}
	return user.Nickname, user.Phone, nil
}
