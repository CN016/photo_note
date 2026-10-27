package love.imcn016.photo;

import static org.junit.Assert.*;
import android.content.Intent;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import androidx.activity.result.contract.ActivityResultContracts;
import org.junit.Test;
import org.junit.runner.RunWith;

/** 覆盖图库、头像、反馈、项目封面及混合附件实际使用的 accept 形式。 */
@RunWith(AndroidJUnit4.class)
public class MediaPickerInstrumentedTest {
    @Test public void imageExtensionsAndMimeOpenGallery() {
        for (String[] accept : new String[][] {
            {"image/*", ".heic", ".heif"}, {"image/jpeg", "image/png", "image/webp", "image/gif"},
            {"image/heic", "image/heif"}, {".HEIC,.JPG,.AVIF"}, {"image/*"}
        }) {
            assertEquals(MediaPicker.Kind.IMAGE, MediaPicker.classify(accept));
            var context = InstrumentationRegistry.getInstrumentation().getTargetContext();
            Intent intent = MediaPicker.createIntent(context, accept, true);
            assertNotEquals(Intent.ACTION_OPEN_DOCUMENT, intent.getAction());
            assertNotNull("设备应有相册处理此入口", intent.resolveActivity(context.getPackageManager()));
            if (!ActivityResultContracts.PickVisualMedia.isPhotoPickerAvailable(context)) {
                assertEquals(Intent.ACTION_PICK, intent.getAction());
                assertEquals("image/*", intent.getType());
                assertTrue(intent.getBooleanExtra(Intent.EXTRA_ALLOW_MULTIPLE, false));
            }
        }
    }

    @Test public void mixedDocumentsAndImportsKeepFilePicker() {
        var context = InstrumentationRegistry.getInstrumentation().getTargetContext();
        for (String[] accept : new String[][] { {"image/*", ".pdf"}, {".json", ".csv", "application/json", "text/csv"},
                {".pdf", ".doc", ".docx", ".xls", ".xlsx"}, {".unknown"}, {}, {""} }) {
            assertEquals(MediaPicker.Kind.DOCUMENT, MediaPicker.classify(accept));
            assertEquals(Intent.ACTION_OPEN_DOCUMENT, MediaPicker.createIntent(context, accept, false).getAction());
        }
        assertEquals(MediaPicker.Kind.DOCUMENT, MediaPicker.classify(null));
    }

    @Test public void videoAndMixedMediaAreDistinguished() {
        assertEquals(MediaPicker.Kind.VIDEO, MediaPicker.classify(new String[]{"video/*", ".mp4"}));
        assertEquals(MediaPicker.Kind.MEDIA, MediaPicker.classify(new String[]{"image/*", "video/*"}));
    }
}
