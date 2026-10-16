BEGIN;

ALTER TABLE inspirations
    ADD COLUMN IF NOT EXISTS image_id text,
    ADD COLUMN IF NOT EXISTS source_file_size bigint,
    ADD COLUMN IF NOT EXISTS source_last_modified_at timestamptz,
    ADD COLUMN IF NOT EXISTS image_width integer,
    ADD COLUMN IF NOT EXISTS image_height integer,
    ADD COLUMN IF NOT EXISTS horizontal_resolution_dpi double precision,
    ADD COLUMN IF NOT EXISTS vertical_resolution_dpi double precision,
    ADD COLUMN IF NOT EXISTS bit_depth integer,
    ADD COLUMN IF NOT EXISTS color_space text,
    ADD COLUMN IF NOT EXISTS compression text,
    ADD COLUMN IF NOT EXISTS title text,
    ADD COLUMN IF NOT EXISTS subject text,
    ADD COLUMN IF NOT EXISTS rating integer,
    ADD COLUMN IF NOT EXISTS metadata_tags text[],
    ADD COLUMN IF NOT EXISTS author text,
    ADD COLUMN IF NOT EXISTS comments text,
    ADD COLUMN IF NOT EXISTS copyright text,
    ADD COLUMN IF NOT EXISTS software text,
    ADD COLUMN IF NOT EXISTS date_acquired text,
    ADD COLUMN IF NOT EXISTS date_time_original text,
    ADD COLUMN IF NOT EXISTS date_time_digitized text,
    ADD COLUMN IF NOT EXISTS camera_make text,
    ADD COLUMN IF NOT EXISTS camera_model text,
    ADD COLUMN IF NOT EXISTS camera_serial_number text,
    ADD COLUMN IF NOT EXISTS lens_make text,
    ADD COLUMN IF NOT EXISTS lens_model text,
    ADD COLUMN IF NOT EXISTS lens_serial_number text,
    ADD COLUMN IF NOT EXISTS flash_make text,
    ADD COLUMN IF NOT EXISTS flash_model text,
    ADD COLUMN IF NOT EXISTS flash_serial_number text,
    ADD COLUMN IF NOT EXISTS aperture_f_number double precision,
    ADD COLUMN IF NOT EXISTS maximum_aperture_f_number double precision,
    ADD COLUMN IF NOT EXISTS exposure_time_seconds double precision,
    ADD COLUMN IF NOT EXISTS iso_speed double precision,
    ADD COLUMN IF NOT EXISTS exposure_compensation_ev double precision,
    ADD COLUMN IF NOT EXISTS exposure_program text,
    ADD COLUMN IF NOT EXISTS flash_fired boolean,
    ADD COLUMN IF NOT EXISTS flash_mode text,
    ADD COLUMN IF NOT EXISTS flash_energy_bcps double precision,
    ADD COLUMN IF NOT EXISTS focal_length_mm double precision,
    ADD COLUMN IF NOT EXISTS focal_length_35mm_mm double precision,
    ADD COLUMN IF NOT EXISTS subject_distance_m double precision,
    ADD COLUMN IF NOT EXISTS metering_mode text,
    ADD COLUMN IF NOT EXISTS light_source text,
    ADD COLUMN IF NOT EXISTS white_balance text,
    ADD COLUMN IF NOT EXISTS contrast text,
    ADD COLUMN IF NOT EXISTS brightness_ev double precision,
    ADD COLUMN IF NOT EXISTS saturation text,
    ADD COLUMN IF NOT EXISTS sharpness text,
    ADD COLUMN IF NOT EXISTS digital_zoom_ratio double precision,
    ADD COLUMN IF NOT EXISTS compression_bits_per_pixel double precision,
    ADD COLUMN IF NOT EXISTS exif_version text,
    ADD COLUMN IF NOT EXISTS gps_latitude double precision,
    ADD COLUMN IF NOT EXISTS gps_longitude double precision,
    ADD COLUMN IF NOT EXISTS gps_altitude_m double precision,
    ADD COLUMN IF NOT EXISTS gps_timestamp text,
    ADD COLUMN IF NOT EXISTS image_metadata_extra jsonb NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN inspirations.image_id IS '图片元数据中的图像标识；按原始标签文本保存，缺失时为空。';
COMMENT ON COLUMN inspirations.source_file_size IS '浏览器读取的原始图片文件大小，单位为字节；不表示服务端压缩工作副本大小。';
COMMENT ON COLUMN inspirations.source_last_modified_at IS '浏览器 File.lastModified 提供的原图修改时间；不包含本机文件创建时间或访问时间。';
COMMENT ON COLUMN inspirations.image_width IS '原始图片像素宽度，单位为像素；读取不到时为空。';
COMMENT ON COLUMN inspirations.image_height IS '原始图片像素高度，单位为像素；读取不到时为空。';
COMMENT ON COLUMN inspirations.horizontal_resolution_dpi IS '图片水平方向分辨率，统一为每英寸点数；原始单位为厘米时转换为 DPI。';
COMMENT ON COLUMN inspirations.vertical_resolution_dpi IS '图片垂直方向分辨率，统一为每英寸点数；原始单位为厘米时转换为 DPI。';
COMMENT ON COLUMN inspirations.bit_depth IS '图片每像素总位深，单位为 bit；多通道每通道位深将求和。';
COMMENT ON COLUMN inspirations.color_space IS '原始图片声明的颜色空间或颜色表示标签。';
COMMENT ON COLUMN inspirations.compression IS '原始图片元数据声明的压缩方式或压缩标签。';
COMMENT ON COLUMN inspirations.title IS '图片元数据中的标题或对象名称。';
COMMENT ON COLUMN inspirations.subject IS '图片元数据中的主题或描述摘要。';
COMMENT ON COLUMN inspirations.rating IS '图片元数据中的原始评分数值，不进行跨软件评分刻度换算。';
COMMENT ON COLUMN inspirations.metadata_tags IS '图片元数据中的关键词或标签数组；每项保存一个原始标签文本。';
COMMENT ON COLUMN inspirations.author IS '图片元数据中的作者、艺术家或创建者文本。';
COMMENT ON COLUMN inspirations.comments IS '图片元数据中的备注、说明或用户注释文本。';
COMMENT ON COLUMN inspirations.copyright IS '图片元数据中的版权声明文本。';
COMMENT ON COLUMN inspirations.software IS '创建或处理原图的软件名称或程序版本文本。';
COMMENT ON COLUMN inspirations.date_acquired IS '图片元数据中的获取日期原始文本；不读取或保存本机文件系统创建时间。';
COMMENT ON COLUMN inspirations.date_time_original IS '拍摄时间原始文本；保留原始格式且不为无时区值推断时区。';
COMMENT ON COLUMN inspirations.date_time_digitized IS '数字化或创建时间原始文本；保留原始格式且不推断时区。';
COMMENT ON COLUMN inspirations.camera_make IS '拍摄设备的相机制造商文本。';
COMMENT ON COLUMN inspirations.camera_model IS '拍摄设备的相机型号文本。';
COMMENT ON COLUMN inspirations.camera_serial_number IS '原图元数据中的相机序列号文本；仅在受鉴权灵感图记录中保存。';
COMMENT ON COLUMN inspirations.lens_make IS '拍摄镜头的制造商文本。';
COMMENT ON COLUMN inspirations.lens_model IS '拍摄镜头的型号文本。';
COMMENT ON COLUMN inspirations.lens_serial_number IS '原图元数据中的镜头序列号文本；仅在受鉴权灵感图记录中保存。';
COMMENT ON COLUMN inspirations.flash_make IS '外接闪光设备的制造商文本。';
COMMENT ON COLUMN inspirations.flash_model IS '外接闪光设备的型号文本。';
COMMENT ON COLUMN inspirations.flash_serial_number IS '原图元数据中的闪光灯序列号文本；仅在受鉴权灵感图记录中保存。';
COMMENT ON COLUMN inspirations.aperture_f_number IS '光圈值，以 F 数表示，例如 2.8。';
COMMENT ON COLUMN inspirations.maximum_aperture_f_number IS '当前焦距下镜头报告的最大光圈值，以 F 数表示。';
COMMENT ON COLUMN inspirations.exposure_time_seconds IS '曝光时间，统一为秒，例如 1/125 秒保存为约 0.008 秒。';
COMMENT ON COLUMN inspirations.iso_speed IS 'ISO 感光度数值；保留数值形式，不转换为相机厂商显示文字。';
COMMENT ON COLUMN inspirations.exposure_compensation_ev IS '曝光补偿值，单位为 EV。';
COMMENT ON COLUMN inspirations.exposure_program IS '相机曝光程序标签或标准化名称。';
COMMENT ON COLUMN inspirations.flash_fired IS '闪光灯是否触发；无法识别状态时为空。';
COMMENT ON COLUMN inspirations.flash_mode IS '闪光灯模式原始说明或标准化模式文本。';
COMMENT ON COLUMN inspirations.flash_energy_bcps IS '闪光灯能量，单位为光束烛光秒（BCPS）；ExifTool 返回的 EXIF 原始数值按此单位保存。';
COMMENT ON COLUMN inspirations.focal_length_mm IS '镜头实际焦距，单位为毫米。';
COMMENT ON COLUMN inspirations.focal_length_35mm_mm IS '等效 35 毫米焦距，单位为毫米。';
COMMENT ON COLUMN inspirations.subject_distance_m IS '相机测得的主体距离，单位为米。';
COMMENT ON COLUMN inspirations.metering_mode IS '相机测光模式标签或标准化名称。';
COMMENT ON COLUMN inspirations.light_source IS '相机记录的光源或白平衡光源标签。';
COMMENT ON COLUMN inspirations.white_balance IS '相机记录的白平衡模式标签。';
COMMENT ON COLUMN inspirations.contrast IS '相机或图像元数据记录的对比度标签。';
COMMENT ON COLUMN inspirations.brightness_ev IS '图像元数据记录的亮度值，单位为 EV。';
COMMENT ON COLUMN inspirations.saturation IS '相机或图像元数据记录的饱和度标签。';
COMMENT ON COLUMN inspirations.sharpness IS '相机或图像元数据记录的清晰度标签。';
COMMENT ON COLUMN inspirations.digital_zoom_ratio IS '拍摄时数字变焦倍率；1 表示未进行数字变焦。';
COMMENT ON COLUMN inspirations.compression_bits_per_pixel IS '图片压缩位率，单位为 bit/像素。';
COMMENT ON COLUMN inspirations.exif_version IS '原始图片中记录的 EXIF 版本文本。';
COMMENT ON COLUMN inspirations.gps_latitude IS '原图 GPS 纬度，十进制度数，北纬为正、南纬为负。';
COMMENT ON COLUMN inspirations.gps_longitude IS '原图 GPS 经度，十进制度数，东经为正、西经为负。';
COMMENT ON COLUMN inspirations.gps_altitude_m IS '原图 GPS 海拔高度，单位为米；正负方向遵循元数据标记。';
COMMENT ON COLUMN inspirations.gps_timestamp IS '原图 GPS 日期和时间原始文本；保留原始格式，不推断本地时区。';
COMMENT ON COLUMN inspirations.image_metadata_extra IS '未映射到固定列的带命名空间图片元数据 JSON 对象；序列化大小上限为 1 MiB。';

COMMIT;
