-- 合并同一用户的重复标签，并将灵感图中的重复或别名标签 UUID 映射为规范标签。
WITH ranked_tags AS (
    SELECT business_id,
           first_value(business_id) OVER (
               PARTITION BY user_id, content
               ORDER BY created_at, business_id
           ) AS canonical_business_id
    FROM tags
), mapped_tags AS (
    SELECT inspiration.business_id AS inspiration_business_id,
           COALESCE(ranked_tags.canonical_business_id, original.tag_id) AS tag_id,
           MIN(original.ordinality) AS first_position
    FROM inspirations AS inspiration
    LEFT JOIN LATERAL unnest(inspiration.tag_ids) WITH ORDINALITY AS original(tag_id, ordinality) ON TRUE
    LEFT JOIN ranked_tags ON ranked_tags.business_id = original.tag_id
    GROUP BY inspiration.business_id, COALESCE(ranked_tags.canonical_business_id, original.tag_id)
), normalized_inspirations AS (
    SELECT inspiration_business_id,
           COALESCE(
               array_agg(tag_id ORDER BY first_position) FILTER (WHERE tag_id IS NOT NULL),
               ARRAY[]::uuid[]
           ) AS tag_ids
    FROM mapped_tags
    GROUP BY inspiration_business_id
)
UPDATE inspirations AS inspiration
SET tag_ids = normalized_inspirations.tag_ids,
    updated_at = CURRENT_TIMESTAMP
FROM normalized_inspirations
WHERE inspiration.business_id = normalized_inspirations.inspiration_business_id
  AND inspiration.tag_ids IS DISTINCT FROM normalized_inspirations.tag_ids;

WITH ranked_tags AS (
    SELECT business_id,
           first_value(business_id) OVER (
               PARTITION BY user_id, content
               ORDER BY created_at, business_id
           ) AS canonical_business_id
    FROM tags
)
DELETE FROM tags AS duplicate
USING ranked_tags
WHERE duplicate.business_id = ranked_tags.business_id
  AND ranked_tags.business_id <> ranked_tags.canonical_business_id;

CREATE UNIQUE INDEX ux_tags_user_content
    ON tags (user_id, content);
