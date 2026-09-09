ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS image_input_price_per_image NUMERIC(20,12);

ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS image_input_price_per_image NUMERIC(20,12);

ALTER TABLE channel_pricing_intervals
    ADD COLUMN IF NOT EXISTS max_pixels BIGINT;

ALTER TABLE channel_account_stats_pricing_intervals
    ADD COLUMN IF NOT EXISTS max_pixels BIGINT;

ALTER TABLE bytedance_image_executions
    ADD COLUMN IF NOT EXISTS input_image_price NUMERIC(24,12) NOT NULL DEFAULT 0;

ALTER TABLE bytedance_image_executions
    ADD COLUMN IF NOT EXISTS input_image_count INTEGER NOT NULL DEFAULT 0;

INSERT INTO model_intros (model_key,title,description,description_en,default_params,output_fields,result_field,result_type,enabled)
VALUES
('bytedance/seedream-v5.0-pro/edit','Seedream 5.0 Pro Edit',
 '参考图片编辑，至少需要一张输入图片。',
 'Edit with one or more reference images.',
 '{"prompt":{"value":"","required":true,"widget":"textarea"},"image":{"items":{"value":"","widget":"image"},"value":[],"minItems":1,"maxItems":10,"widget":"image-annotations","prompt_field":"prompt"},"size":{"value":"2K"},"output_format":{"value":"jpeg"},"response_format":{"value":"url","enum":true,"options":["url"]},"watermark":{"value":true}}'::jsonb,
 '[{"key":"data[*].url","type":"string","description":"Output images"}]'::jsonb,'data[*].url','image',true),
('bytedance/seedream-v5.0-pro/layer','Seedream 5.0 Pro Layer',
 '图层拆分，必须提供一张输入图片，返回底图和独立图层。',
 'Layer decomposition with exactly one reference image.',
 '{"prompt":{"value":"","required":true,"widget":"textarea"},"image":{"items":{"value":"","widget":"image"},"value":[],"minItems":1,"maxItems":1,"widget":"image-annotations","prompt_field":"prompt"},"size":{"value":"2K"},"output_format":{"value":"jpeg"},"response_format":{"value":"url","enum":true,"options":["url"]},"watermark":{"value":true}}'::jsonb,
 '[{"key":"data[*].url","type":"string","description":"Background and layers"}]'::jsonb,'data[*].url','image',true),
('bytedance/seedream-v5.0-pro/text-to-image','Seedream 5.0 Pro Text to Image',
 '文生图，不接受输入图片。',
 'Text-to-image without reference images.',
 '{"prompt":{"value":"","required":true,"widget":"textarea"},"image":{"items":{"value":"","widget":"image"},"value":[],"maxItems":0,"widget":"image","prompt_field":"prompt"},"size":{"value":"2K"},"output_format":{"value":"jpeg"},"response_format":{"value":"url","enum":true,"options":["url"]},"watermark":{"value":true}}'::jsonb,
 '[{"key":"data[*].url","type":"string","description":"Output images"}]'::jsonb,'data[*].url','image',true)
ON CONFLICT (model_key) DO NOTHING;
