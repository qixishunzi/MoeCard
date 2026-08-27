-- 首页那块写死的色块横幅被轮播图取代了，对应的开关不再有人读。
--
-- 留着一行读不到的配置只会让下次翻这张表的人多花两分钟确认"这是什么"。
DELETE FROM `system_settings` WHERE `setting_key` = 'home_hero_enabled';
