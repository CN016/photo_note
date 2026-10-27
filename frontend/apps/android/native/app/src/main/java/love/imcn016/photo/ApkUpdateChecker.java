package love.imcn016.photo;

import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.ApplicationInfo;
import android.net.Network;
import android.net.Uri;
import org.json.JSONObject;
import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CopyOnWriteArraySet;

/** APK 更新只读取固定官网的版本记录；不下载安装包、不影响独立的签名 Web 热更新。 */
final class ApkUpdateChecker {
    static final long INTERVAL_MS = 60 * 60 * 1000L;
    static final String STATE = "apk-update-state";
    private static final Object CLAIM_LOCK = new Object();
    private static final CopyOnWriteArraySet<Runnable> listeners = new CopyOnWriteArraySet<>();
    private final SharedPreferences state;
    private final URL feed;
    private final URL download;
    private final long installed;
    private final String channel;
    record Release(long code, String version) {}

    /** 配置由构建时官网环境变量生成，不允许服务端版本记录重定向升级按钮。 */
    ApkUpdateChecker(Context context) throws Exception {
        this(context, readConfig(context));
    }
    ApkUpdateChecker(Context context, JSONObject config) throws Exception {
        feed = safeUrl(config.getString("feedUrl")); download = safeUrl(config.getString("downloadUrl"));
        if (!feed.getAuthority().equals(download.getAuthority())) throw new SecurityException("origin");
        installed = context.getPackageManager().getPackageInfo(context.getPackageName(), 0).getLongVersionCode();
        channel = (context.getApplicationInfo().flags & ApplicationInfo.FLAG_DEBUGGABLE) != 0 ? "debug" : "release";
        state = context.getSharedPreferences(STATE, Context.MODE_PRIVATE);
        synchronized (CLAIM_LOCK) {
            if (!feed.toString().equals(state.getString("source", ""))) {
                if (!state.edit().clear().putString("source", feed.toString()).commit()) throw new java.io.IOException("state");
            }
        }
    }

    private static JSONObject readConfig(Context context) throws Exception {
        try (InputStream input = context.getAssets().open("apk-update-config.json")) {
            return new JSONObject(new String(BundleIO.read(input), StandardCharsets.UTF_8));
        }
    }
    /** 仅允许构建时确认的无凭据 HTTPS 地址；拒绝片段、查询和异常主机。 */
    private static URL safeUrl(String value) throws Exception {
        URL url = new URL(value);
        if (!"https".equals(url.getProtocol()) || url.getHost().isBlank() || url.getUserInfo() != null
                || url.getRef() != null || url.getQuery() != null) throw new SecurityException("url");
        return url;
    }
    static void subscribe(Runnable listener) { listeners.add(listener); }
    static void unsubscribe(Runnable listener) { listeners.remove(listener); }

    /** 前台计时器和系统任务共用持久化领取门槛，避免重建 Activity 或并发触发重复请求。 */
    void checkAt(long now, Network network) {
        checkAt(now, network, false);
    }
    void checkAt(long now, Network network, boolean scheduled) {
        synchronized (CLAIM_LOCK) {
            long previous = state.getLong("last_check", 0);
            // 系统任务可在周期末尾五分钟弹性窗口执行，不能因此错过后再等完整一小时。
            long minimum = scheduled ? INTERVAL_MS - ApkUpdateJobService.FLEX_MS : INTERVAL_MS;
            if (previous > 0 && now >= previous && now - previous < minimum) return;
            if (!state.edit().putLong("last_check", now).commit()) return;
        }
        try {
            JSONObject result = new JSONObject(new String(fetch(network), StandardCharsets.UTF_8));
            Release release = parse(result, installed, channel);
            if (Thread.currentThread().isInterrupted() || !feed.toString().equals(state.getString("source", ""))) return;
            var edit = state.edit();
            if (release == null) edit.remove("available_code").remove("available_version");
            else edit.putLong("available_code", release.code()).putString("available_version", release.version());
            if (edit.commit()) for (Runnable listener : listeners) listener.run();
        } catch (Exception ignored) {
            // 离线、旧清单、异常响应只保留上次已确认结果；不弹网络错误、不清理任何登录数据。
        }
    }

    /** 版本号必须是合法整数；以 Android 安装版本比较，渠道不匹配不能诱导跨签名升级。 */
    static Release parse(JSONObject json, long installed, String channel) throws Exception {
        Object rawCode = json.get("versionCode");
        if (json.getInt("schema") != 1 || !(rawCode instanceof Number number)
                || number.longValue() <= 0 || number.longValue() > Integer.MAX_VALUE
                || number.doubleValue() != number.longValue()) throw new java.io.IOException("version_code");
        String version = json.getString("version");
        if (!version.matches("[0-9]{1,9}\\.[0-9]{1,9}\\.[0-9]{1,9}")) throw new java.io.IOException("version");
        if (!channel.equals(json.getString("channel")) || number.longValue() <= installed) return null;
        return new Release(number.longValue(), version);
    }

    /** 固定 HTTPS、禁止重定向和缓存，限制 64KiB 响应及连接/读取超时，不发送账号凭据。 */
    private byte[] fetch(Network network) throws Exception {
        HttpURLConnection connection = (HttpURLConnection)(network == null ? feed.openConnection() : network.openConnection(feed));
        connection.setInstanceFollowRedirects(false); connection.setUseCaches(false);
        connection.setConnectTimeout(4000); connection.setReadTimeout(6000);
        connection.setRequestProperty("Cache-Control", "no-cache"); connection.setRequestProperty("Accept", "application/json");
        try {
            if (connection.getResponseCode() != 200 || connection.getContentLengthLong() > 65536) throw new java.io.IOException("response");
            long deadline = android.os.SystemClock.elapsedRealtime() + 10000;
            try (InputStream input = connection.getInputStream(); ByteArrayOutputStream output = new ByteArrayOutputStream()) {
                byte[] bytes = new byte[4096];
                for (int count; (count = input.read(bytes)) != -1;) {
                    if (Thread.currentThread().isInterrupted() || output.size() + count > 65536
                            || android.os.SystemClock.elapsedRealtime() > deadline) throw new java.io.IOException("limit");
                    output.write(bytes, 0, count);
                }
                return output.toByteArray();
            }
        } finally { connection.disconnect(); }
    }

    /** 同版本最多每小时提示一次；新版本可立即提示，稍后/跳转/旋转均不造成连续弹窗。 */
    Release pending(long now) {
        long code = state.getLong("available_code", 0), prompted = state.getLong("prompted_code", 0), at = state.getLong("prompted_at", 0);
        String version = state.getString("available_version", "");
        if (code <= installed || version == null || !version.matches("[0-9]{1,9}\\.[0-9]{1,9}\\.[0-9]{1,9}")) return null;
        if (code == prompted && now >= at && now - at < INTERVAL_MS) return null;
        return new Release(code, version);
    }
    void prompted(Release release, long now) { state.edit().putLong("prompted_code", release.code()).putLong("prompted_at", now).apply(); }
    long nextDelay(long now) {
        long previous = state.getLong("last_check", 0);
        return previous <= 0 || previous > now ? 1000 : Math.max(1000, INTERVAL_MS - (now - previous));
    }
    Intent downloadIntent() { return new Intent(Intent.ACTION_VIEW, Uri.parse(download.toString())).addCategory(Intent.CATEGORY_BROWSABLE); }
}
