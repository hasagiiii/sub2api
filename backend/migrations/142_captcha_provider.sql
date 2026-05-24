INSERT INTO settings (key, value)
VALUES
    ('captcha_provider', 'turnstile'),
    ('captcha_config', '{}')
ON CONFLICT (key) DO NOTHING;
