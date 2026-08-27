-- 0005: 管理员头像
--
-- 空字符串表示没设头像，前端回落到用户名首字母那块色卡。
-- 存的是图片地址（站内 /uploads/... 或外链），不是图片本身 ——
-- 头像走的是和商品封面同一套上传接口，没必要再造一份存储。
ALTER TABLE `admins` ADD COLUMN `avatar` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '头像地址，空=用首字母';
