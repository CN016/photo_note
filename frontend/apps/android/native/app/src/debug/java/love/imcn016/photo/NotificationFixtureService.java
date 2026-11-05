package love.imcn016.photo;
import android.content.ContextWrapper;
import java.io.File;
import org.json.JSONObject;

/** 调试构建专用的离线通知源：隔离文件、合成令牌和固定JSON，不连接任何业务后端。 */
public final class NotificationFixtureService extends NotificationMonitorService {
    private int requests;
    @Override protected SessionVault createVault(String ignored) throws Exception {
        File directory = new File(getCacheDir(), "notification-fixture");
        if (!directory.exists() && !directory.mkdirs()) throw new java.io.IOException("fixture_directory");
        SessionVault vault = new SessionVault(new ContextWrapper(this) { @Override public File getFilesDir() { return directory; } }, "https://fixture.invalid");
        vault.accept(new JSONObject().put("session_id", "fixture-notification").put("session_version", 1).put("orgid", -1)
            .put("refresh_token", "fixture-refresh").put("access_token", "fixture-access").put("expires_in", 3600));
        return vault;
    }
    @Override protected JSONObject fetch(String access) throws Exception {
        if (!"fixture-access".equals(access)) throw new SecurityException("fixture_token");
        return new JSONObject().put("items", new org.json.JSONArray().put(new JSONObject().put("business_id", ++requests == 1 ? "history" : "new-message"))).put("unread_count", 1);
    }
}
