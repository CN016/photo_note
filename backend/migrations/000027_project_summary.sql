-- 人工复盘与策划正文使用相同修订号保护，历史策划初始化为空。
ALTER TABLE projects ADD COLUMN manual_summary text NOT NULL DEFAULT '';
COMMENT ON COLUMN projects.manual_summary IS '策划人工总结内容；空白表示尚未填写，不包含 AI 自动分析结果';
