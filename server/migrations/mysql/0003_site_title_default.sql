-- 让「页面标题」默认跟随商城名称。
--
-- 之前 site_title 的出厂默认值是一句写死的宣传语，而 site_name 由安装向导写入。
-- 结果是：店主把商城改名成「萌卡商城」，页头也确实变了，
-- 浏览器标签页却始终显示出厂那句 "MoeCard - 数字商品自动发货商城" ——
-- 因为标题优先取 site_title，而它从没被人动过。
--
-- 只清掉「一字未改的出厂值」，自己填过标题的店铺不受影响。
UPDATE `system_settings`
SET `value` = '', `updated_at` = `updated_at`
WHERE `setting_key` = 'site_title'
  AND `value` = 'MoeCard - 数字商品自动发货商城';
