BEGIN;
ALTER TABLE inspiration_processing_jobs ADD COLUMN analysis_ready boolean NOT NULL DEFAULT false;
COMMENT ON COLUMN inspiration_processing_jobs.analysis_ready IS '是否允许AI领取；仅初次新增或每日未识别扫描置为真，换图与浏览不触发AI，已有任务默认等待每日扫描。';
CREATE INDEX ix_inspiration_processing_analysis_ready ON inspiration_processing_jobs(stage,next_attempt_at,id) WHERE analysis_ready=true;
COMMENT ON INDEX ix_inspiration_processing_analysis_ready IS '受控系统worker跨空间领取已许可任务的部分索引，避免反复扫描等待每日处理的未许可记录；不用于普通用户检索。';
INSERT INTO app_configs(business_id,orgid,config_key,config_value,description,is_public) VALUES(gen_random_uuid(),-1,'files.thumbnails','{"dimension":384,"quality":78,"max_pixels":40000000,"workers":2}','私有图片持久缩略图参数；仅执行图片缩放编码，不调用AI模型。',false);
COMMIT;
