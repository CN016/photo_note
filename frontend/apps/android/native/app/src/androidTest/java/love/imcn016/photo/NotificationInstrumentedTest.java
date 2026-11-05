package love.imcn016.photo;
import android.app.NotificationManager;
import android.content.Context;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.junit.Test;
import org.junit.runner.RunWith;
import static org.junit.Assert.*;

/** 使用合成通知验证真实系统渠道和通知栏；不登录、不访问业务后端。 */
@RunWith(AndroidJUnit4.class)
public class NotificationInstrumentedTest {
    @Test public void nativePollingContinuesAfterAppGoesToBackground() throws Exception {
        var instrumentation = InstrumentationRegistry.getInstrumentation();
        Context context = instrumentation.getTargetContext();
        if (android.os.Build.VERSION.SDK_INT >= 33) instrumentation.getUiAutomation().grantRuntimePermission(context.getPackageName(), android.Manifest.permission.POST_NOTIFICATIONS);
        android.app.Activity activity = instrumentation.startActivitySync(new android.content.Intent(context, MainActivity.class).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK));
        var intent = new android.content.Intent().setClassName(context, "love.imcn016.photo.NotificationFixtureService").putExtra("identity", "fixture-notification:-1:1");
        NotificationManager manager = context.getSystemService(NotificationManager.class);
        manager.cancel(NotificationMonitorService.MESSAGE_ID);
        try {
            context.startForegroundService(intent);
            long deadline = android.os.SystemClock.elapsedRealtime() + 10000;
            while (java.util.Arrays.stream(manager.getActiveNotifications()).noneMatch(item -> item.getId() == NotificationMonitorService.MONITOR_ID) && android.os.SystemClock.elapsedRealtime() < deadline) Thread.sleep(100);
            assertTrue(java.util.Arrays.stream(manager.getActiveNotifications()).anyMatch(item -> item.getId() == NotificationMonitorService.MONITOR_ID));
            androidx.test.uiautomator.UiDevice.getInstance(instrumentation).pressHome();
            deadline = android.os.SystemClock.elapsedRealtime() + 45000;
            while (java.util.Arrays.stream(manager.getActiveNotifications()).noneMatch(item -> item.getId() == NotificationMonitorService.MESSAGE_ID) && android.os.SystemClock.elapsedRealtime() < deadline) Thread.sleep(200);
            assertTrue("退到后台后原生服务应完成第二次轮询并生成消息通知", java.util.Arrays.stream(manager.getActiveNotifications()).anyMatch(item -> item.getId() == NotificationMonitorService.MESSAGE_ID));
        } finally { context.stopService(intent); manager.cancel(NotificationMonitorService.MESSAGE_ID); instrumentation.runOnMainSync(activity::finish); }
    }
    @Test public void systemChannelAndNotificationUseGenericPrivateContent() throws Exception {
        Context context = InstrumentationRegistry.getInstrumentation().getTargetContext();
        NotificationMonitorService.channels(context);
        NotificationManager manager = context.getSystemService(NotificationManager.class);
        assertNotNull(manager.getNotificationChannel(NotificationMonitorService.CHANNEL));
        assertEquals(NotificationManager.IMPORTANCE_LOW, manager.getNotificationChannel(NotificationMonitorService.MONITOR_CHANNEL).getImportance());
        var notification = NotificationMonitorService.message(context);
        assertNotNull(notification.contentIntent);
        assertEquals(android.app.Notification.VISIBILITY_PRIVATE, notification.visibility);
        if (android.os.Build.VERSION.SDK_INT >= 33) InstrumentationRegistry.getInstrumentation().getUiAutomation().grantRuntimePermission(context.getPackageName(), android.Manifest.permission.POST_NOTIFICATIONS);
        try {
            manager.notify(4199, notification);
            // 系统通知入队是异步的，等待真实发布完成，避免把队列延迟误判成功能失败。
            long deadline = android.os.SystemClock.elapsedRealtime() + 5000;
            while (java.util.Arrays.stream(manager.getActiveNotifications()).noneMatch(item -> item.getId() == 4199) && android.os.SystemClock.elapsedRealtime() < deadline) Thread.sleep(100);
            assertTrue(java.util.Arrays.stream(manager.getActiveNotifications()).anyMatch(item -> item.getId() == 4199));
        } finally { manager.cancel(4199); }
    }
}
