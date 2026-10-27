package love.imcn016.photo;

import android.content.Context;
import android.content.Intent;
import android.provider.MediaStore;
import android.webkit.MimeTypeMap;
import androidx.activity.result.PickVisualMediaRequest;
import androidx.activity.result.contract.ActivityResultContracts;
import java.util.Locale;
import java.util.LinkedHashSet;

/** 统一识别网页 accept 的 MIME 和扩展名；纯媒体打开相册，未知或混合文档保持文件选择器。 */
final class MediaPicker {
    enum Kind { IMAGE, VIDEO, MEDIA, DOCUMENT }

    /** 空声明、未知扩展名和任何非媒体项都不得误入相册；扩展名不依赖设备 MIME 数据库。 */
    static Kind classify(String[] accepts) {
        if (accepts == null || accepts.length == 0) return Kind.DOCUMENT;
        boolean images = false, videos = false;
        for (String value : accepts) {
            if (value == null || value.isBlank()) return Kind.DOCUMENT;
            for (String part : value.toLowerCase(Locale.ROOT).split(",", -1)) {
                String type = normalize(part.trim());
                if (type.startsWith("image/")) images = true;
                else if (type.startsWith("video/")) videos = true;
                else return Kind.DOCUMENT;
            }
        }
        return images && videos ? Kind.MEDIA : images ? Kind.IMAGE : videos ? Kind.VIDEO : Kind.DOCUMENT;
    }

    /** 补齐 HEIC/HEIF 等系统可能尚未登记的格式，其他扩展名再查询 Android MIME 映射。 */
    private static String normalize(String value) {
        if (!value.startsWith(".")) return value;
        return switch (value) {
            case ".heic", ".heif", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp" -> "image/" + value.substring(1);
            case ".mp4", ".webm", ".mov", ".m4v", ".3gp" -> "video/" + value.substring(1);
            default -> { String type = MimeTypeMap.getSingleton().getMimeTypeFromExtension(value.substring(1)); yield type == null ? "" : type; }
        };
    }

    /** 新系统使用无需整库权限的 Photo Picker；旧系统显式打开相册，保留单选/多选语义。 */
    static Intent createIntent(Context context, String[] accepts, boolean multiple) {
        Kind kind = classify(accepts);
        if (kind == Kind.DOCUMENT) {
            Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE)
                .setType("*/*").putExtra(Intent.EXTRA_ALLOW_MULTIPLE, multiple);
            LinkedHashSet<String> types = new LinkedHashSet<>();
            if (accepts != null) for (String value : accepts) if (value != null)
                for (String part : value.split(",")) { String type = normalize(part.trim().toLowerCase(Locale.ROOT)); if (type.contains("/")) types.add(type); }
            if (types.size() == 1) intent.setType(types.iterator().next());
            else if (!types.isEmpty()) intent.putExtra(Intent.EXTRA_MIME_TYPES, types.toArray(new String[0]));
            return intent;
        }
        if (ActivityResultContracts.PickVisualMedia.isPhotoPickerAvailable(context)) {
            var mediaType = kind == Kind.IMAGE ? ActivityResultContracts.PickVisualMedia.ImageOnly.INSTANCE
                : kind == Kind.VIDEO ? ActivityResultContracts.PickVisualMedia.VideoOnly.INSTANCE
                : ActivityResultContracts.PickVisualMedia.ImageAndVideo.INSTANCE;
            var request = new PickVisualMediaRequest.Builder().setMediaType(mediaType).build();
            return multiple ? new ActivityResultContracts.PickMultipleVisualMedia().createIntent(context, request)
                : new ActivityResultContracts.PickVisualMedia().createIntent(context, request);
        }
        // 不回退到文件管理器；旧厂商相册可能仅支持单选，回调仍接受合法的单项结果。
        return new Intent(Intent.ACTION_PICK)
            .setDataAndType(kind == Kind.VIDEO ? MediaStore.Video.Media.EXTERNAL_CONTENT_URI : MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
                kind == Kind.IMAGE ? "image/*" : kind == Kind.VIDEO ? "video/*" : "*/*")
            .putExtra(Intent.EXTRA_MIME_TYPES, kind == Kind.MEDIA ? new String[]{"image/*", "video/*"}
                : new String[]{kind == Kind.IMAGE ? "image/*" : "video/*"})
            .putExtra(Intent.EXTRA_ALLOW_MULTIPLE, multiple);
    }
}
