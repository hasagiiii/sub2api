ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE user_platform_quotas ADD CONSTRAINT user_platform_quotas_platform_check
CHECK (platform IN ('anthropic','openai','gemini','antigravity','kiro','grok','fal','leonardo','atlascloud','apiz','higgsfield','kimi','zhipu','deepseek','bytedance'));

-- The provider execution record is separate from the legacy queue-provider state.
CREATE TABLE IF NOT EXISTS bytedance_image_executions (
    task_id BIGINT PRIMARY KEY REFERENCES async_media_tasks(id) ON DELETE CASCADE,
    request_payload JSONB NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','running','result_ready','settled','billing_failed','refunded')),
    billing_type SMALLINT NOT NULL,
    unit_price NUMERIC(24,12) NOT NULL,
    billable_images INTEGER,
    started_at TIMESTAMPTZ,
    result_payload JSONB,
    billing_error TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO model_intros (model_key,title,description,description_en,default_params,output_fields,result_field,result_type,enabled)
VALUES (
 'doubao-seedream-5-0-pro-260628','Doubao Seedream 5.0 Pro',
 '支持文生图、单图生图、多图生图（2–10 张参考图生成单张图片）。支持通过坐标、框选、箭头定位编辑。图层拆分仅接受单张输入，返回免费底图及独立图层；按 16 个图层预扣，按实际收费图层数退差或补扣。不支持组图生成、sequential_image_generation、联网搜索和流式输出。',
 'Text-to-image, single-reference and multi-reference (2–10 images) generation produce one image. Edit with coordinates, boxes and arrows. Layer decomposition accepts one reference and returns a free background and separate layers. Reserve 16 layers and settle the actual billable count. Sequential generation, web search and streaming are not supported.',
 '{
 "prompt":{"value":"","required":true,"widget":"textarea","description":"生成或编辑指令","description_en":"Generation or editing instructions"},
 "image":{"items":{"value":"","widget":"image"},"value":[],"maxItems":10,"widget":"image-annotations","prompt_field":"prompt","description":"参考图片；图层拆分只能输入一张图片","description_en":"Reference images; layer decomposition requires one image"},
 "layer_decomposition":{"value":false,"description":"图层拆分","description_en":"Layer decomposition"},
 "size":{"value":"2K"},"output_format":{"value":"jpeg"},
 "response_format":{"value":"url","enum":true,"options":["url"]},"watermark":{"value":true}
 }'::jsonb,
 '[{"key":"data[*].url","type":"string","description":"Output images"},
 {"key":"data","type":"array","description":"Background and layers","items":{"type":"object","properties":{"url":{"type":"string"},"size":{"type":"string"},"output_format":{"type":"string"},"z_index":{"type":"number"},"bounding_box":{"type":"object","properties":{"absolute":{"type":"array","items":{"type":"number"}},"normalized":{"type":"array","items":{"type":"number"}}}},"name":{"type":"string"},"description":{"type":"string"}}}},
 {"key":"usage","type":"object","description":"Provider usage","properties":{"input_images":{"type":"number"},"generated_images":{"type":"number"},"output_tokens":{"type":"number"},"total_tokens":{"type":"number"}}}]'::jsonb,
 'data[*].url','image',true
) ON CONFLICT (model_key) DO NOTHING;
