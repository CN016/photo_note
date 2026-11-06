package love.imcn016.photo;

import static org.junit.Assert.*;
import android.app.Activity;
import android.app.Instrumentation;
import android.app.job.JobScheduler;
import android.content.Intent;
import android.content.SharedPreferences;
import android.os.SystemClock;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import androidx.test.uiautomator.*;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import java.util.regex.Pattern;

/** 使用合成版本状态，不下载安装包；拦截外部 Intent，防止测试真的离开应用访问官网。 */
@RunWith(AndroidJUnit4.class)
public class ApkUpdateInstrumentedTest {
    /** 离线夹具验证每次进入强制检查、在途合并及弹窗节流；不访问官网。 */
    @Test public void entryChecksBypassHourButCoalesceAndRespectPromptDismissal() throws Exception {
        var app = InstrumentationRegistry.getInstrumentation().getTargetContext();
        String prefix = "apk-entry-fixture-" + java.util.UUID.randomUUID();
        var context = new android.content.ContextWrapper(app) {
            @Override public SharedPreferences getSharedPreferences(String name, int mode) { return super.getSharedPreferences(prefix + name, mode); }
        };
        long code = app.getPackageManager().getPackageInfo(app.getPackageName(), 0).getLongVersionCode() + 1;
        byte[] release = new JSONObject().put("schema", 1).put("versionCode", code).put("version", "9.9.9").put("channel", "debug").toString().getBytes(java.nio.charset.StandardCharsets.UTF_8);
        var requests = new java.util.concurrent.atomic.AtomicInteger();
        var entered = new java.util.concurrent.CountDownLatch(1); var finish = new java.util.concurrent.CountDownLatch(1);
        var checker = new ApkUpdateChecker(context, new JSONObject().put("feedUrl", "https://fixture.invalid/entry.json").put("downloadUrl", "https://fixture.invalid/download"), network -> {
            if (requests.incrementAndGet() == 3) { entered.countDown(); if (!finish.await(5, java.util.concurrent.TimeUnit.SECONDS)) throw new java.io.IOException("fixture_timeout"); }
            return release;
        });
        long now = System.currentTimeMillis();
        checker.checkAt(now, null); checker.checkAt(now + 1000, null); assertEquals(1, requests.get());
        checker.checkOnEntry(now + 2000); assertEquals("每次进入绕过小时缓存", 2, requests.get());
        checker.prompted(checker.pending(now + 2000), now + 2000);
        Thread request = new Thread(() -> checker.checkOnEntry(now + 3000)); request.start();
        try {
            assertTrue(entered.await(5, java.util.concurrent.TimeUnit.SECONDS)); checker.checkOnEntry(now + 4000);
            assertEquals("在途检查不得并发重复请求", 3, requests.get());
        } finally { finish.countDown(); request.join(5000); }
        assertFalse(request.isAlive()); assertNull("稍后后重新检查仍不得重复弹窗", checker.pending(now + 5000));
    }

    @Test public void versionCodeChannelAndMalformedRecords() throws Exception {
        JSONObject release = new JSONObject().put("schema", 1).put("versionCode", 12).put("version", "1.3.0").put("channel", "debug");
        assertEquals(12, ApkUpdateChecker.parse(release, 11, "debug").code());
        assertNull(ApkUpdateChecker.parse(release, 12, "debug"));
        assertNull(ApkUpdateChecker.parse(release, 13, "debug"));
        assertNull(ApkUpdateChecker.parse(release, 11, "release"));
        for (Object code : new Object[]{"13", 1.5, 0, -1, 2147483648L}) {
            try { ApkUpdateChecker.parse(release.put("versionCode", code), 1, "debug"); fail("异常安装版本必须拒绝"); } catch (java.io.IOException expected) {}
        }
        try { ApkUpdateChecker.parse(release.put("versionCode", 13).put("version", "<bad>"), 1, "debug"); fail(); } catch (java.io.IOException expected) {}
    }

    @Test public void hourlyJobDialogAndExternalBrowser() throws Exception {
        var instrumentation = InstrumentationRegistry.getInstrumentation(); var context = instrumentation.getTargetContext();
        // 进入时的真实异步检查不能覆盖合成版本，弹窗验收使用独立状态域。
        String prefix="apk-dialog-fixture-"+java.util.UUID.randomUUID();
        var fixtureContext=new android.content.ContextWrapper(context) {
            @Override public SharedPreferences getSharedPreferences(String name,int mode) { return super.getSharedPreferences(prefix+name,mode); }
        };
        long code = context.getPackageManager().getPackageInfo(context.getPackageName(), 0).getLongVersionCode() + 1;
        var checker = new ApkUpdateChecker(fixtureContext,new JSONObject().put("feedUrl","https://fixture.invalid/release.json").put("downloadUrl","https://fixture.invalid/download"), network -> new JSONObject().put("schema",1).put("versionCode",code).put("version","9.9.9").put("channel","debug").toString().getBytes(java.nio.charset.StandardCharsets.UTF_8));
        SharedPreferences state = fixtureContext.getSharedPreferences(ApkUpdateChecker.STATE, 0);
        Map<String, ?> previous = state.getAll();
        state.edit().putLong("available_code", code).putString("available_version", "9.9.9")
            .putLong("last_check", System.currentTimeMillis()).remove("prompted_code").remove("prompted_at").commit();
        AtomicReference<Intent> opened = new AtomicReference<>();
        Instrumentation.ActivityMonitor browser = new Instrumentation.ActivityMonitor() {
            @Override public Instrumentation.ActivityResult onStartActivity(Intent intent) {
                if (Intent.ACTION_VIEW.equals(intent.getAction())) { opened.set(intent); return new Instrumentation.ActivityResult(Activity.RESULT_CANCELED, null); }
                return null;
            }
        };
        instrumentation.addMonitor(browser);
        MainActivity activity = null;
        try {
            activity = (MainActivity) instrumentation.startActivitySync(new Intent(context, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
            MainActivity fixtureActivity=activity;
            instrumentation.runOnMainSync(() -> {
                try { var field=MainActivity.class.getDeclaredField("apkUpdates");field.setAccessible(true);field.set(fixtureActivity,checker); }
                catch(Exception failure){throw new AssertionError(failure);}
            });
            var device = UiDevice.getInstance(instrumentation);
            var later = device.wait(Until.findObject(By.text(Pattern.compile("稍后再说|Later", Pattern.CASE_INSENSITIVE))), 45000);
            assertNotNull("发现更新应出现原生弹窗", later); later.click();
            MainActivity current = activity;
            instrumentation.runOnMainSync(current::showApkUpdate);
            assertFalse("稍后后立即返回不能重复弹窗", device.hasObject(By.text(Pattern.compile("稍后再说|Later", Pattern.CASE_INSENSITIVE))));
            assertNull(checker.pending(System.currentTimeMillis()));
            state.edit().putLong("prompted_at", System.currentTimeMillis() - ApkUpdateChecker.INTERVAL_MS).commit();
            instrumentation.runOnMainSync(current::showApkUpdate);
            var upgrade = device.wait(Until.findObject(By.text(Pattern.compile("前往官网下载|Open download page", Pattern.CASE_INSENSITIVE))), 5000);
            assertNotNull(upgrade); upgrade.click();
            long deadline = SystemClock.uptimeMillis() + 5000;
            while (opened.get() == null && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(100);
            assertNotNull("升级按钮必须发出外部浏览器 Intent", opened.get());
            assertEquals(checker.downloadIntent().getData(), opened.get().getData());
            assertTrue(opened.get().hasCategory(Intent.CATEGORY_BROWSABLE));
            var job = context.getSystemService(JobScheduler.class).getPendingJob(ApkUpdateJobService.JOB_ID);
            assertNotNull(job); assertTrue(job.isPeriodic()); assertTrue(job.isPersisted());
            assertEquals(ApkUpdateChecker.INTERVAL_MS, job.getIntervalMillis());
            assertEquals(android.app.job.JobInfo.NETWORK_TYPE_ANY, job.getNetworkType());
        } finally {
            if (activity != null) { MainActivity current = activity; instrumentation.runOnMainSync(current::finish); }
            instrumentation.removeMonitor(browser);
            var edit = state.edit().clear();
            for (var item : previous.entrySet()) {
                if (item.getValue() instanceof String value) edit.putString(item.getKey(), value);
                else if (item.getValue() instanceof Long value) edit.putLong(item.getKey(), value);
            }
            edit.commit();
        }
    }
}
