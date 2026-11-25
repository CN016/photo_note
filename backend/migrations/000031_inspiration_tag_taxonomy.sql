BEGIN;

ALTER TABLE tags
    ADD COLUMN tag_type varchar(16) NOT NULL DEFAULT 'custom',
    ADD COLUMN category varchar(32) NOT NULL DEFAULT 'custom';

ALTER TABLE tags
    ADD CONSTRAINT chk_tags_type CHECK (tag_type IN ('system', 'custom')),
    ADD CONSTRAINT chk_tags_category CHECK (category IN ('subject', 'composition', 'lighting', 'style', 'custom')),
    ADD CONSTRAINT chk_tags_system_owner CHECK (
        tag_type <> 'system' OR (orgid = -1 AND user_id = '00000000-0000-0000-0000-000000000000'::uuid)
    ),
    ADD CONSTRAINT chk_tags_custom_owner CHECK (
        tag_type <> 'custom' OR user_id <> '00000000-0000-0000-0000-000000000000'::uuid
    );

CREATE UNIQUE INDEX ux_tags_system_content
    ON tags (content)
    WHERE tag_type = 'system';
CREATE INDEX ix_tags_visible_category
    ON tags (orgid, user_id, tag_type, category);

INSERT INTO tags (business_id, orgid, user_id, content, tag_type, category)
VALUES
    ('c672825b-b3da-55cc-8b83-67ec8e257378', -1, '00000000-0000-0000-0000-000000000000', '单人', 'system', 'subject'),
    ('b4acdb5c-2048-5e2a-8725-a4552468d8b0', -1, '00000000-0000-0000-0000-000000000000', '男孩', 'system', 'subject'),
    ('e29686e9-9f20-54d3-8fc5-29e86739a964', -1, '00000000-0000-0000-0000-000000000000', '女孩', 'system', 'subject'),
    ('24093de5-0b56-5f67-8c34-ae5628422de7', -1, '00000000-0000-0000-0000-000000000000', '兄妹', 'system', 'subject'),
    ('aa3f9291-f662-5f67-bc60-c4c6cdf2e694', -1, '00000000-0000-0000-0000-000000000000', '姐弟', 'system', 'subject'),
    ('834a712d-c000-5fb4-8ca4-6e8a1c38f121', -1, '00000000-0000-0000-0000-000000000000', '一家三口', 'system', 'subject'),
    ('0cde1882-7734-514d-9946-59e04a1760cb', -1, '00000000-0000-0000-0000-000000000000', '四口之家', 'system', 'subject'),
    ('b87048d2-fdff-51ed-a715-bc743d39e420', -1, '00000000-0000-0000-0000-000000000000', '周岁宝宝', 'system', 'subject'),
    ('3bdbcd0d-31ec-5b92-9e0c-cf1e55d04718', -1, '00000000-0000-0000-0000-000000000000', '孕妈', 'system', 'subject'),
    ('566e2090-06c8-56d5-9de2-092d4c3c7d46', -1, '00000000-0000-0000-0000-000000000000', '情侣', 'system', 'subject'),
    ('9b3fc51b-9740-566f-849b-d6dc73ebdb8b', -1, '00000000-0000-0000-0000-000000000000', '宠物', 'system', 'subject'),
    ('7fcf5ab0-ad97-5be6-880f-e9e9521c6d28', -1, '00000000-0000-0000-0000-000000000000', '老人', 'system', 'subject'),
    ('19eb12b5-db46-5c8d-8827-d054acc78446', -1, '00000000-0000-0000-0000-000000000000', '竖构图', 'system', 'composition'),
    ('19144335-ec6b-5a9c-933e-12b5375bbbb8', -1, '00000000-0000-0000-0000-000000000000', '横构图', 'system', 'composition'),
    ('0a4b923d-460f-55b4-bf5d-b173bd3ace01', -1, '00000000-0000-0000-0000-000000000000', '半身', 'system', 'composition'),
    ('2cf62e23-1305-5782-91c8-c3ec51c7f739', -1, '00000000-0000-0000-0000-000000000000', '特写', 'system', 'composition'),
    ('23bcbdec-9c97-5863-a27a-bdb65995ee30', -1, '00000000-0000-0000-0000-000000000000', '大头', 'system', 'composition'),
    ('45d80190-5c49-5e12-9bd7-95f24a78cf1a', -1, '00000000-0000-0000-0000-000000000000', '全景', 'system', 'composition'),
    ('ab90eb01-4e6b-58a6-9b6c-1e5024d07b28', -1, '00000000-0000-0000-0000-000000000000', '框架构图', 'system', 'composition'),
    ('ca34aa47-6c63-5d17-ad38-24277e72db7b', -1, '00000000-0000-0000-0000-000000000000', '对角线构图', 'system', 'composition'),
    ('2da9a15a-48a7-535c-ad8c-dbd8b59b3e68', -1, '00000000-0000-0000-0000-000000000000', '前景虚化', 'system', 'composition'),
    ('cae693e9-71e9-53f2-9f9e-420c194d0f04', -1, '00000000-0000-0000-0000-000000000000', '侧逆光', 'system', 'lighting'),
    ('0ce78ed3-8f18-535a-b4ec-d13c38baed4f', -1, '00000000-0000-0000-0000-000000000000', '顺光', 'system', 'lighting'),
    ('aebee700-e658-59b1-8f73-69c530330fd7', -1, '00000000-0000-0000-0000-000000000000', '树荫柔光', 'system', 'lighting'),
    ('8175c1e4-b67c-5165-ae57-7c2217ff0874', -1, '00000000-0000-0000-0000-000000000000', '黄金时刻', 'system', 'lighting'),
    ('e2d1e2b0-9287-5172-9892-5861e67c2211', -1, '00000000-0000-0000-0000-000000000000', '阴天漫射光', 'system', 'lighting'),
    ('d48261a0-25f4-5a1c-ac1a-86b66db3a825', -1, '00000000-0000-0000-0000-000000000000', '窗边光', 'system', 'lighting'),
    ('679aef87-e99c-57eb-ae2b-2fb3334e7a4a', -1, '00000000-0000-0000-0000-000000000000', '海边', 'system', 'lighting'),
    ('fb9de38c-f127-5258-a2f7-1fe0e323bc88', -1, '00000000-0000-0000-0000-000000000000', '树林', 'system', 'lighting'),
    ('c995e495-d200-504d-a414-ec2e12de38c8', -1, '00000000-0000-0000-0000-000000000000', '草坪', 'system', 'lighting'),
    ('383c89ed-a96a-59f1-a68e-c7a31bea913d', -1, '00000000-0000-0000-0000-000000000000', '室内', 'system', 'lighting'),
    ('3fd80703-7969-5d37-963b-c8879528faa3', -1, '00000000-0000-0000-0000-000000000000', '夜景', 'system', 'lighting'),
    ('c4b54b0f-98e6-5371-a120-63e0e14fa8a1', -1, '00000000-0000-0000-0000-000000000000', '青色', 'system', 'style'),
    ('c15b3d31-e3b8-5f52-af1e-a0d3d3f68d60', -1, '00000000-0000-0000-0000-000000000000', '日系', 'system', 'style'),
    ('86b47846-c871-5c74-8b8b-cd6e4cb25871', -1, '00000000-0000-0000-0000-000000000000', '胶片', 'system', 'style'),
    ('3ac85753-75c8-5822-898f-8b0dc31bc108', -1, '00000000-0000-0000-0000-000000000000', '暖调', 'system', 'style'),
    ('ccc24c70-0ba4-5cd9-892e-d5ca724ee166', -1, '00000000-0000-0000-0000-000000000000', '低饱和', 'system', 'style'),
    ('bd7b54fd-101d-5af3-8ef9-2ff4413afd62', -1, '00000000-0000-0000-0000-000000000000', '清透', 'system', 'style'),
    ('f9c80e4e-ca4d-598f-9d8d-d2a214dfe907', -1, '00000000-0000-0000-0000-000000000000', '纪实', 'system', 'style'),
    ('68c1ba6f-0171-5ed5-859d-b6a9d5e3fb75', -1, '00000000-0000-0000-0000-000000000000', '古风', 'system', 'style'),
    ('74d96431-bf94-5437-9dc7-4b3f5ee7c40f', -1, '00000000-0000-0000-0000-000000000000', '法式', 'system', 'style'),
    ('af577610-a052-56fc-a05f-1e95d1befd35', -1, '00000000-0000-0000-0000-000000000000', '奶油感', 'system', 'style')
ON CONFLICT (business_id) DO UPDATE SET
    orgid = EXCLUDED.orgid,
    user_id = EXCLUDED.user_id,
    content = EXCLUDED.content,
    tag_type = EXCLUDED.tag_type,
    category = EXCLUDED.category,
    updated_at = CURRENT_TIMESTAMP;

ALTER TABLE projects
    ADD COLUMN shoot_keywords text[] NOT NULL DEFAULT ARRAY[]::text[];

COMMENT ON COLUMN tags.tag_type IS '标签类型：system 表示全局系统预设，custom 表示当前用户私有自定义标签。';
COMMENT ON COLUMN tags.category IS '标签分类：subject 主体、composition 构图景别、lighting 光影场景、style 色调风格、custom 用户自定义。';
COMMENT ON INDEX ux_tags_system_content IS '系统标签名称全局唯一，避免不同分类生成同名预设。';
COMMENT ON COLUMN projects.shoot_keywords IS '拍摄关键词数组，最多 20 项，每项最多 64 个字符；用于从灵感标签带入策划。';

COMMIT;
