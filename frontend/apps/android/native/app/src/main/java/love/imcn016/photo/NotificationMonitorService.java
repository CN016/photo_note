package love.imcn016.photo;

import android.app.*;
import android.content.*;
import android.content.pm.ServiceInfo;
import android.os.*;
import org.json.JSONObject;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.*;

/** 默认随登录启动的HTTP消息监听。前台服务提高后台存活率，但不绕过强停、省电或系统运行时限。 */
public class NotificationMonitorService extends Service {
    static final String CHANNEL = "messages-v1", MONITOR_CHANNEL = "message-monitor-v1";
    static final String OPEN_MESSAGES = "love.imcn016.photo.OPEN_MESSAGES";
    static final int MONITOR_ID = 4101, MESSAGE_ID = 4102;
    private final ScheduledExecutorService worker = Executors.newSingleThreadScheduledExecutor();
    private SessionVault vault;
    private String apiBase;
    private volatile String expected = "";
    private NotificationBaseline baseline = new NotificationBaseline();
    private int failures;
    private ScheduledFuture<?> next;

    /** 渠道默认开启；用户在系统设置关闭渠道或权限后，不擅自恢复。 */
    static void channels(Context context) {
        NotificationManager manager = context.getSystemService(NotificationManager.class);
        manager.createNotificationChannel(new NotificationChannel(CHANNEL, context.getString(R.string.notice_channel), NotificationManager.IMPORTANCE_DEFAULT));
        manager.createNotificationChannel(new NotificationChannel(MONITOR_CHANNEL, context.getString(R.string.notice_monitor_channel), NotificationManager.IMPORTANCE_LOW));
    }
    private static PendingIntent open(Context context, boolean messages) {
        Intent intent = new Intent(context, MainActivity.class).setAction(messages ? OPEN_MESSAGES : Intent.ACTION_MAIN)
            .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        return PendingIntent.getActivity(context, messages ? MESSAGE_ID : MONITOR_ID, intent, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
    }
    /** 锁屏只显示通用提醒，不将站内正文、图片或内部诊断交给系统通知栏。 */
    static Notification message(Context context) {
        return new Notification.Builder(context, CHANNEL).setSmallIcon(android.R.drawable.ic_dialog_info)
            .setContentTitle(context.getString(R.string.notice_new)).setContentText(context.getString(R.string.notice_open))
            .setContentIntent(open(context, true)).setAutoCancel(true).setVisibility(Notification.VISIBILITY_PRIVATE).build();
    }
    @Override public void onCreate() {
        super.onCreate(); channels(this);
        try {
            try (var input = getAssets().open("update-config.json")) { apiBase = new JSONObject(new String(SessionVault.readLimited(input), StandardCharsets.UTF_8)).getString("apiOrigin"); }
            if (!apiBase.startsWith("https://")) throw new SecurityException("invalid_origin");
            vault = createVault(apiBase);
        } catch (Exception unavailable) { stopSelf(); }
    }
    /** 调试夹具可替换隔离凭据；正式服务仅使用APK配置和通知专属加密保管库。 */
    protected SessionVault createVault(String origin) throws Exception { return NotificationCredentials.get(this, origin).vault; }
    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        if (vault == null || intent == null) { stopSelf(); return START_NOT_STICKY; }
        String identity = intent.getStringExtra("identity");
        try {
            // 主线程必须先及时进入前台，不等待可能正在执行网络刷新的凭据锁；worker再校验会话。
            if (identity == null || identity.isEmpty()) { stopSelf(); return START_NOT_STICKY; }
            Notification status = new Notification.Builder(this, MONITOR_CHANNEL).setSmallIcon(android.R.drawable.ic_dialog_info)
                .setContentTitle(getString(R.string.notice_monitor_channel)).setContentText(getString(R.string.notice_monitor_running))
                .setContentIntent(open(this, false)).setOngoing(true).setOnlyAlertOnce(true).build();
            if (Build.VERSION.SDK_INT >= 29) startForeground(MONITOR_ID, status, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC);
            else startForeground(MONITOR_ID, status);
            // 服务重复启动不重复排程；空间变化时立即废弃旧轮询结果，并重新建立基线。
            if (!identity.equals(expected)) {
                expected = identity;
                getSystemService(NotificationManager.class).cancel(MESSAGE_ID);
                worker.execute(() -> { baseline = new NotificationBaseline(); failures = 0; if (next != null) next.cancel(false); poll(); });
            }
        } catch (Exception unavailable) { stopSelf(); }
        return START_NOT_STICKY;
    }
    /** 单线程串行轮询和退避；失败不推进基线，首次只建立基线，后续将多条变化合并为一条提醒。 */
    private void poll() {
        String identity = expected;
        try {
            if (!identity.equals(vault.identity())) { stopSelf(); return; }
            JSONObject tokens = vault.refresh(identity, false);
            JSONObject page = fetch(tokens.getString("access_token"));
            if (page == null) page = fetch(vault.refresh(identity, true).getString("access_token"));
            if (page == null) { stopSelf(); return; }
            synchronized (vault) {
                if (!identity.equals(expected) || !identity.equals(vault.identity())) return;
                var items = page.getJSONArray("items");
                String id = items.length() == 0 ? "" : items.getJSONObject(0).getString("business_id");
                if (baseline.update(id, page.optLong("unread_count"))) {
                    if (Build.VERSION.SDK_INT < 33 || checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) == android.content.pm.PackageManager.PERMISSION_GRANTED) {
                        getSystemService(NotificationManager.class).notify(MESSAGE_ID, message(this));
                    }
                }
                if (page.optLong("unread_count") == 0) getSystemService(NotificationManager.class).cancel(MESSAGE_ID);
            }
            failures = 0;
        } catch (SecurityException revoked) { stopSelf(); return;
        } catch (Exception transientFailure) { failures = Math.min(failures + 1, 4); }
        if (!worker.isShutdown() && identity.equals(expected)) next = worker.schedule(this::poll, Math.min(300, 30L << failures), TimeUnit.SECONDS);
    }
    /** 固定接口和HTTPS来源，禁止重定向及客户端任意URL；不打印响应、令牌和用户正文。 */
    protected JSONObject fetch(String access) throws Exception {
        HttpURLConnection connection = (HttpURLConnection) new URL(apiBase + "/api/v1/notifications?page=1&page_size=1&unread_only=false").openConnection();
        connection.setInstanceFollowRedirects(false); connection.setConnectTimeout(10000); connection.setReadTimeout(15000);
        connection.setRequestProperty("Authorization", "Bearer " + access);
        try {
            int status = connection.getResponseCode();
            if (status == 401) return null;
            if (status == 403) throw new SecurityException("notification_forbidden");
            if (status != 200) throw new java.io.IOException("notification_unavailable");
            try (var input = connection.getInputStream()) { return new JSONObject(new String(SessionVault.readLimited(input), StandardCharsets.UTF_8)).getJSONObject("data"); }
        } finally { connection.disconnect(); }
    }
    /** Android 15+同步服务后台累计时限到达后必须及时退出；仅用户再次打开应用时尝试恢复。 */
    @Override public void onTimeout(int startId, int fgsType) { stopForeground(STOP_FOREGROUND_REMOVE); stopSelf(); }
    @Override public void onDestroy() { expected = ""; worker.shutdownNow(); stopForeground(STOP_FOREGROUND_REMOVE); super.onDestroy(); }
    @Override public IBinder onBind(Intent intent) { return null; }
}
