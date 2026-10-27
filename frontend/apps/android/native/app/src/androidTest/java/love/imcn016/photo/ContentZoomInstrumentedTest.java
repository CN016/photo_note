package love.imcn016.photo;

import static org.junit.Assert.*;
import android.content.Intent;
import android.os.SystemClock;
import android.view.MotionEvent;
import androidx.test.uiautomator.*;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.mozilla.geckoview.*;
import java.util.concurrent.atomic.AtomicReference;

/** 在真实 Gecko 中加载生产页面及主体组件，用 UIAutomator 双指触控和只读调试扩展测量缩放边界。 */
@RunWith(AndroidJUnit4.class)
public class ContentZoomInstrumentedTest {
    /** 真实触控点使用屏幕像素坐标，压力和尺寸保持有效，避免被系统判为悬停。 */
    private static MotionEvent.PointerCoords point(int x, int y) {
        var p = new MotionEvent.PointerCoords(); p.x = x; p.y = y; p.pressure = 1; p.size = 1; return p;
    }
    @Test public void onlyProductionContentScales() throws Exception {
        var instrumentation = InstrumentationRegistry.getInstrumentation();
        MainActivity activity = (MainActivity) instrumentation.startActivitySync(new Intent(instrumentation.getTargetContext(), MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
        AtomicReference<JSONObject> result = new AtomicReference<>();
        AtomicReference<JSONObject> ready = new AtomicReference<>();
        try {
            long deadline = SystemClock.uptimeMillis() + 45000;
            while (!activity.hasNativePageConnection() && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(200);
            assertTrue(activity.hasNativePageConnection());
            instrumentation.runOnMainSync(() -> MainActivity.getRuntime(activity).getWebExtensionController()
                .ensureBuiltIn("resource://android/assets/zoom-test/", "photo-nini-zoom-tests@imcn016.love").accept(extension -> {
                    WebExtension.MessageDelegate delegate = new WebExtension.MessageDelegate() {
                        @Override public GeckoResult<Object> onMessage(String name, Object message, WebExtension.MessageSender sender) {
                            JSONObject data = (JSONObject) message;
                            if ("ready".equals(data.optString("type"))) ready.set(data); else result.set(data);
                            return GeckoResult.fromValue(true);
                        }
                    };
                    extension.setMessageDelegate(delegate, "zoom_test");
                    activity.getSession().getWebExtensionController().setMessageDelegate(extension, delegate, "zoom_test");
                    activity.getSession().reload();
                }, error -> { try { result.set(new JSONObject().put("error", "extension_failed")); } catch (Exception ignored) {} }));
            deadline = SystemClock.uptimeMillis() + 45000;
            while (ready.get() == null && result.get() == null && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(100);
            assertNotNull("生产主体已完成布局后再注入双指触控", ready.get());
            var device = UiDevice.getInstance(instrumentation);
            device.wakeUp(); device.waitForIdle();
            // DOM 就绪早于 Gecko 首帧提交；等待真实控件可访问，避免把手势打到尚未绘制的 HTML 根节点。
            assertNotNull(device.wait(Until.findObject(By.clazz("android.widget.Button").pkg("love.imcn016.photo")), 10000));
            SystemClock.sleep(500);
            MotionEvent.PointerCoords[] left = new MotionEvent.PointerCoords[25], right = new MotionEvent.PointerCoords[25];
            int cx = device.getDisplayWidth() / 2, cy = device.getDisplayHeight() / 2;
            for (int i = 0; i < left.length; i++) {
                left[i] = point(cx - 80 - i * 4, cy); right[i] = point(cx + 80 + i * 4, cy);
            }
            assertTrue(device.findObject(new UiSelector().packageName("love.imcn016.photo")).performMultiPointerGesture(left, right));
            while (result.get() == null && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(100);
            assertNotNull("生产页缩放测量应完成", result.get());
            JSONObject data = result.get();
            android.util.Log.i("ContentZoomTest", data.toString());
            assertFalse(data.toString(), data.has("error"));
            assertEquals(1, data.getDouble("scale"), 0.01);
            double zoom = Double.parseDouble(data.getString("zoom"));
            assertTrue(data.toString(), zoom > 1.2 && zoom <= 3);
            assertEquals(data.getInt("viewportWidth"), data.getInt("pageWidth"));
            for (String part : new String[]{"header", "footer"}) {
                JSONObject before = data.getJSONObject("before").getJSONObject(part), after = data.getJSONObject("after").getJSONObject(part);
                assertEquals(before.getDouble("width"), after.getDouble("width"), 0.1);
                assertEquals(before.getDouble("height"), after.getDouble("height"), 0.1);
            }
            assertEquals(data.toString(), data.getJSONObject("before").getJSONObject("main").getDouble("width") * zoom,
                data.getJSONObject("after").getJSONObject("main").getDouble("width"), 1);
        } finally { instrumentation.runOnMainSync(activity::finish); }
    }
}
