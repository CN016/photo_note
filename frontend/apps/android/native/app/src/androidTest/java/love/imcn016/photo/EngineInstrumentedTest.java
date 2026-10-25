package love.imcn016.photo;

import static org.junit.Assert.*;
import android.content.Intent;
import android.os.SystemClock;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import androidx.test.uiautomator.*;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.mozilla.geckoview.*;
import java.util.concurrent.atomic.AtomicReference;

/** 使用仅调试包携带的扩展验证真实 Gecko Canvas 和文件选择；不向生产页注入测试脚本。 */
@RunWith(AndroidJUnit4.class)
public class EngineInstrumentedTest {
    @Test public void bundledGeckoCanvasAndFilePicker() throws Exception {
        var instrumentation = InstrumentationRegistry.getInstrumentation();
        var device = UiDevice.getInstance(instrumentation);
        MainActivity activity = (MainActivity) instrumentation.startActivitySync(new Intent(instrumentation.getTargetContext(), MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
        long initialDeadline = SystemClock.uptimeMillis() + 45000;
        while (!activity.hasNativePageConnection() && SystemClock.uptimeMillis() < initialDeadline) SystemClock.sleep(200);
        assertTrue("先等待生产页启动，避免其迟到回调覆盖合成测试页", activity.hasNativePageConnection());
        AtomicReference<JSONObject> ready = new AtomicReference<>();
        AtomicReference<JSONObject> canvas = new AtomicReference<>();
        AtomicReference<String> failure = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> MainActivity.getRuntime(activity).getWebExtensionController()
            .ensureBuiltIn("resource://android/assets/engine-test/", "photo-nini-tests@imcn016.love").accept(extension -> {
                WebExtension.MessageDelegate delegate = new WebExtension.MessageDelegate() {
                    @Override public GeckoResult<Object> onMessage(String name, Object message, WebExtension.MessageSender sender) {
                        JSONObject data = (JSONObject) message;
                        if ("ready".equals(data.optString("type"))) ready.set(data);
                        if ("canvas".equals(data.optString("type"))) canvas.set(data);
                        return GeckoResult.fromValue(true);
                    }
                };
                extension.setMessageDelegate(delegate, "engine_test");
                activity.getSession().getWebExtensionController().setMessageDelegate(extension, delegate, "engine_test");
                activity.getSession().setNavigationDelegate(null);
                activity.getSession().loadUri(extension.metaData.baseUrl + "index.html");
            }, error -> failure.set(error.getMessage())));
        try {
            long deadline = SystemClock.uptimeMillis() + 45000;
            while (ready.get() == null && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(200);
            assertNotNull("内置 Gecko 应加载合成页面：" + failure.get(), ready.get());
            assertTrue(ready.get().optString("userAgent").contains("Gecko/"));
            var button = device.wait(Until.findObject(By.text("Create canvas test")), 10000);
            assertNotNull(button);
            button.click();
            deadline = SystemClock.uptimeMillis() + 30000;
            while (canvas.get() == null && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(200);
            assertNotNull("4096² Canvas 编码及解码应完成", canvas.get());
            assertEquals(4096, canvas.get().optInt("width"));
            assertEquals(4096, canvas.get().optInt("height"));
            assertTrue(canvas.get().optInt("bytes") > 0);
            var file = device.wait(Until.findObject(By.clazz("android.widget.Button").text(java.util.regex.Pattern.compile("(?i).*browse.*|.*choose.*|.*浏览.*|.*选择.*"))), 5000);
            assertNotNull("系统文件选择入口应存在", file);
            file.click();
            assertNotNull(device.wait(Until.findObject(By.pkg(java.util.regex.Pattern.compile(".*documentsui")).depth(0)), 10000));
            device.pressBack();
        } finally { instrumentation.runOnMainSync(activity::finish); }
    }
}
