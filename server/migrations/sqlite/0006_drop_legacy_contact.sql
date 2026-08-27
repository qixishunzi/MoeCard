-- 0006: 老的单值 contact 并进 contacts，然后删掉
--
-- 后台里那个"联系方式（旧字段）"输入框已经被「客服联系方式」列表取代。
-- 留着一个只读不写、还在背地里给前台兜底的字段，结果是站长以为自己
-- 删干净了，页脚却还挂着一个老邮箱 —— 而且没有任何界面能改它。
--
-- 迁移策略：只在 contacts 还是空的时候搬，搬完就删。已经配过新列表的
-- 站点什么都不会变。
--
-- 类型判断和之前 Go 里的兜底逻辑一致：含 @ 当邮箱，否则当微信号
-- （动作是"复制"，对任何内容都不会出错）。
-- 值里的反斜杠和双引号做了转义；正常的邮箱 / 微信号 / QQ 号不会出现
-- 控制字符，真出现了 JSON 解析会失败，前台按"没有联系方式"处理，
-- 不会把页面搞崩。

UPDATE system_settings
SET value = '[{"type":"' ||
            CASE
                WHEN (SELECT value FROM system_settings WHERE setting_key = 'contact')
                     LIKE '%@%' THEN 'email'
                ELSE 'wechat'
            END ||
            '","value":"' ||
            replace(
                replace(
                    (SELECT value FROM system_settings WHERE setting_key = 'contact'),
                    '\', '\\'),
                '"', '\"') ||
            '"}]'
WHERE setting_key = 'contacts'
  AND (value IS NULL OR trim(value) = '' OR trim(value) = '[]')
  AND EXISTS (SELECT 1 FROM system_settings
              WHERE setting_key = 'contact' AND trim(value) <> '');

-- contacts 那一行可能压根不存在（老版本没写过），补一条
INSERT INTO system_settings (setting_key, value, is_secret, updated_at)
SELECT 'contacts',
       '[{"type":"' ||
       CASE WHEN value LIKE '%@%' THEN 'email' ELSE 'wechat' END ||
       '","value":"' || replace(replace(value, '\', '\\'), '"', '\"') || '"}]',
       0,
       updated_at
FROM system_settings
WHERE setting_key = 'contact'
  AND trim(value) <> ''
  AND NOT EXISTS (SELECT 1 FROM system_settings WHERE setting_key = 'contacts');

DELETE FROM system_settings WHERE setting_key = 'contact';
