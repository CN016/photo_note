BEGIN;

ALTER TABLE feedbacks
    ADD COLUMN reply_content text NOT NULL DEFAULT '',
    ADD COLUMN reply_image_keys text[] NOT NULL DEFAULT ARRAY[]::text[];

COMMENT ON COLUMN feedbacks.reply_content IS '反馈中心管理员针对该条用户反馈保存的回复正文，去除首尾空白后受 feedback.max_text_chars 配置限制；空字符串表示当前没有文字回复。';
COMMENT ON COLUMN feedbacks.reply_image_keys IS '管理员回复附带的公开图片 storekey 数组，最多保存五个经实际图片内容校验的 public key；移除图片不会删除可能已被通知引用的文件。';

COMMIT;
