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
--
-- MySQL 不允许在 UPDATE 的子查询里读同一张表，所以先把旧值抄进临时表。

CREATE TEMPORARY TABLE `moecard_legacy_contact` AS
SELECT `value` AS `v`, `updated_at` AS `at`
FROM `system_settings`
WHERE `setting_key` = 'contact' AND TRIM(`value`) <> '';

UPDATE `system_settings`
SET `value` = (
    SELECT CONCAT('[{"type":"',
                  IF(`v` LIKE '%@%', 'email', 'wechat'),
                  '","value":"',
                  REPLACE(REPLACE(`v`, '\\', '\\\\'), '"', '\\"'),
                  '"}]')
    FROM `moecard_legacy_contact` LIMIT 1)
WHERE `setting_key` = 'contacts'
  AND (`value` IS NULL OR TRIM(`value`) = '' OR TRIM(`value`) = '[]')
  AND EXISTS (SELECT 1 FROM `moecard_legacy_contact`);

-- contacts 那一行可能压根不存在（老版本没写过），补一条
INSERT INTO `system_settings` (`setting_key`, `value`, `is_secret`, `updated_at`)
SELECT 'contacts',
       CONCAT('[{"type":"', IF(`v` LIKE '%@%', 'email', 'wechat'),
              '","value":"', REPLACE(REPLACE(`v`, '\\', '\\\\'), '"', '\\"'), '"}]'),
       0,
       `at`
FROM `moecard_legacy_contact`
WHERE NOT EXISTS (SELECT 1 FROM `system_settings` WHERE `setting_key` = 'contacts');

DROP TEMPORARY TABLE `moecard_legacy_contact`;

DELETE FROM `system_settings` WHERE `setting_key` = 'contact';
