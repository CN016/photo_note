package love.imcn016.photo;

import static org.junit.Assert.*;
import android.content.Intent;
import org.json.JSONObject;

import android.os.SystemClock;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import androidx.test.uiautomator.By;
import androidx.test.uiautomator.UiDevice;
import androidx.test.uiautomator.Until;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;

/** 只在独立模拟器上验证壳和合成文件；不登录、不读取真实用户数据、不修改生产服务。 */
@RunWith(AndroidJUnit4.class)
public class ShellInstrumentedTest {
    private MainActivity activity;
    private UiDevice device;
    private NativeShellService shell;
    private final AtomicReference<String> exportResult = new AtomicReference<>("pending");

    @Before
    public void launch() throws Exception {
        var instrumentation = InstrumentationRegistry.getInstrumentation();
        device = UiDevice.getInstance(instrumentation);
        Intent intent = new Intent(instrumentation.getTargetContext(), MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
        activity = (MainActivity) instrumentation.startActivitySync(intent);
        shell = activity.getShell();
        // 留存独立原生更新页外观，使用合成模拟器且尚未登录任何账号。
        device.wait(Until.hasObject(By.pkg("love.imcn016.photo")), 5000);
        device.waitForIdle(300);
        device.takeScreenshot(new java.io.File(activity.getCacheDir(), "update-screen.png"));
        long readyDeadline = SystemClock.uptimeMillis() + 45000;
        while (activity.getSession() == null && SystemClock.uptimeMillis() < readyDeadline) SystemClock.sleep(200);
    }

    @After
    public void finish() { if (activity != null) InstrumentationRegistry.getInstrumentation().runOnMainSync(activity::finish); }

    /** 随包 Gecko 内核应能启动，主题桥接无需设备 WebView。 */
    @Test
    public void bundledEngineAndNativeTheme() throws Exception {
        assertNotNull(activity.getSession());
        long deadline = SystemClock.uptimeMillis() + 45000;
        while (!activity.hasNativePageConnection() && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(200);
        assertTrue("真实共享应用应建立受来源校验的原生连接", activity.hasNativePageConnection());
        JSONObject theme = new JSONObject(); theme.put("dark", true);
        shell.setTheme(testCall("setTheme", theme));
        awaitResult("ok");
        assertEquals("love.imcn016.photo", activity.getPackageName());
    }

    /** 越界文件名必须在显示面板之前拒绝，不能创建任意缓存路径。 */
    @Test
    public void rejectsPathTraversal() throws Exception {
        JSONObject data = new JSONObject();
        data.put("filename", "../secret"); data.put("mime", "text/plain"); data.put("data", "eA==");
        shell.exportFile(testCall("exportFile", data));
        awaitResult("rejected");
    }

    /** 原生服务测试使用合成载荷；不绕过生产扩展的来源授权。 */
    private NativeShellService.Call testCall(String method, JSONObject data) {
        exportResult.set("pending");
        return new NativeShellService.Call(data, new NativeShellService.Reply() {
            @Override public void success(JSONObject result) { exportResult.set(result.optString("status", "ok")); }
            @Override public void failure(String message) { exportResult.set("rejected"); }
        });
    }

    private void awaitResult(String expected) {
        long deadline = SystemClock.uptimeMillis() + 15000;
        while (!expected.equals(exportResult.get()) && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(100);
        assertEquals(expected, exportResult.get());
    }

    private void openExport(String filename, String content, String mime) throws Exception {
        JSONObject data = new JSONObject();
        data.put("filename", filename); data.put("mime", mime); data.put("data", content);
        data.put("title", "Export test"); data.put("save", "Save test"); data.put("share", "Share test"); data.put("cancel", "Cancel test");
        shell.exportFile(testCall("exportFile", data));
        assertNotNull(device.wait(Until.findObject(By.text("Export test")), 10000));
    }

    @Test
    public void cancelExportDoesNotReportSuccess() throws Exception {
        openExport("photo-nini-cancel.json", "e30=", "application/json");
        device.pressBack();
        awaitResult("cancelled");
    }

    /** 合成 JSON 经 SAF 实际保存；结果只在原生写入并关闭文件后报告。 */
    @Test
    public void saveThroughSystemPicker() throws Exception {
        openExport("photo-nini-smoke.json", "e30=", "application/json");
        device.findObject(By.text("Save test")).click();
        assertNotNull(device.wait(Until.findObject(By.pkg(java.util.regex.Pattern.compile(".*documentsui")).depth(0)), 10000));
        var save = device.wait(Until.findObject(By.res(java.util.regex.Pattern.compile(".*documentsui:id/button1"))), 10000);
        if (save == null) save = device.wait(Until.findObject(By.text(java.util.regex.Pattern.compile("(?i)save|保存"))), 5000);
        assertNotNull("系统保存按钮应可用", save);
        save.click();
        awaitResult("saved");
    }

    @Test
    public void opensSystemShareSheet() throws Exception {
        openExport("photo-nini-share.csv", "YSxiCg==", "text/csv");
        device.findObject(By.text("Share test")).click();
        awaitResult("shared");
        device.pressBack();
    }

}
