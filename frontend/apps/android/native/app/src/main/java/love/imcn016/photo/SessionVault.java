package love.imcn016.photo;

import android.content.Context;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.AtomicFile;
import android.util.Base64;
import org.json.JSONObject;
import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;

/** 用Android Keystore和AES-GCM保存刷新凭据；不依赖浏览器异步落盘，不持久化Access Token或密码。 */
final class SessionVault {
    private static SessionVault shared;
    private JSONObject memoryTokens;
    private long accessDeadline;
    /** 页面代理和通知服务共用同一把凭据锁，防止单次刷新令牌被两端并发消费。 */
    static synchronized SessionVault shared(Context context, String apiBase) {
        if (shared == null || !shared.apiBase.equals(apiBase)) shared = new SessionVault(context.getApplicationContext(), apiBase);
        return shared;
    }
    private final AtomicFile file;
    private final String apiBase;
    private final String alias;
    private final java.util.concurrent.atomic.AtomicLong generation = new java.util.concurrent.atomic.AtomicLong();
    private boolean active = true;
    SessionVault(Context context, String apiBase) {
        this.apiBase = apiBase;
        alias = context.getPackageName() + ".refresh-v1";
        file = new AtomicFile(new File(context.getFilesDir(), "session-vault-v1.json"));
    }
    /** 登录、刷新、切换空间的成功响应必须先原子落盘，才能转交网页消费新凭据。 */
    long generation() { return generation.get(); }
    synchronized void stop() { active = false; generation.incrementAndGet(); }
    synchronized void accept(JSONObject tokens) throws Exception { accept(tokens,generation.get(),true); }
    synchronized void accept(JSONObject tokens, long expectedGeneration) throws Exception { accept(tokens,expectedGeneration,false); }
    synchronized void accept(JSONObject tokens, long expectedGeneration, boolean explicitSignIn) throws Exception {
        if (!active || expectedGeneration != generation.get()) return;
        String refresh = tokens.getString("refresh_token"), session = tokens.getString("session_id");
        long org = tokens.getLong("orgid"), version = tokens.getLong("session_version");
        if (refresh.isEmpty() || refresh.length() > 4096 || session.isEmpty() || session.length() > 128 || version <= 0 || version > 9007199254740991L || (org != -1 && (org <= 0 || org > 9007199254740991L))) throw new SecurityException("invalid_session");
        JSONObject value = new JSONObject().put("refresh_token",refresh).put("session_id",session).put("orgid",org).put("session_version",version);
        JSONObject current = read();
        // 退出后的刷新重试不能复活会话；只有新的显式登录/注册成功才能覆盖退出墓碑。
        if (current != null && current.optBoolean("loggedOut") && !explicitSignIn) return;
        JSONObject previous = current == null ? null : current.optJSONObject("session");
        // 同会话迟到的旧空间代次不得覆盖已经持久化的新空间。
        if (previous != null && session.equals(previous.optString("session_id")) && version < previous.optLong("session_version")) return;
        write(new JSONObject().put("session", value));
        memoryTokens = new JSONObject(tokens.toString());
        accessDeadline = android.os.SystemClock.elapsedRealtime() + Math.max(0, tokens.optLong("expires_in") - 30) * 1000;
    }
    /** 显式退出在网络请求前持久化墓碑，离线退出也不能在重启时恢复旧凭据。 */
    synchronized void logout() throws Exception { generation.incrementAndGet(); memoryTokens = null; write(new JSONObject().put("loggedOut",true)); }
    /** 关闭页面代理只废弃旧请求；后台服务仍可使用同一份加密凭据。 */
    void invalidateRequests() { generation.incrementAndGet(); }
    synchronized String identity() throws Exception {
        JSONObject saved = read();
        JSONObject session = saved == null ? null : saved.optJSONObject("session");
        return session == null ? "" : session.getString("session_id") + ":" + session.getLong("orgid") + ":" + session.getLong("session_version");
    }
    /** 切换空间时以原生持久化的最新刷新令牌为准；调用方必须先核对完整会话标记。 */
    synchronized String currentRefresh() throws Exception { return read().getJSONObject("session").getString("refresh_token"); }
    /** 退出请求优先使用后台已轮换的内存Access Token，避免网页旧令牌导致服务端撤销失败。 */
    synchronized String currentAccess() { return memoryTokens == null ? "" : memoryTokens.optString("access_token"); }
    /** 原生和Web统一刷新入口；仅允许当前会话代次，不持久化Access Token，不跟随重定向。 */
    synchronized JSONObject refresh(String expected, boolean force) throws Exception {
        if (!active || expected.isEmpty() || !expected.equals(identity())) throw new SecurityException("session_changed");
        if (!force && memoryTokens != null && android.os.SystemClock.elapsedRealtime() < accessDeadline) return new JSONObject(memoryTokens.toString());
        JSONObject saved = read().getJSONObject("session");
        String refresh = saved.getString("refresh_token");
        java.net.HttpURLConnection connection = (java.net.HttpURLConnection) new java.net.URL(apiBase + "/api/v1/auth/refresh").openConnection();
        connection.setInstanceFollowRedirects(false); connection.setConnectTimeout(10000); connection.setReadTimeout(15000);
        connection.setRequestMethod("POST"); connection.setRequestProperty("Content-Type", "application/json"); connection.setDoOutput(true);
        try {
            try (OutputStream output = connection.getOutputStream()) { output.write(new JSONObject().put("refresh_token", refresh).toString().getBytes(StandardCharsets.UTF_8)); }
            int status = connection.getResponseCode();
            InputStream stream = status >= 400 ? connection.getErrorStream() : connection.getInputStream();
            JSONObject envelope;
            try (InputStream input = stream) { envelope = new JSONObject(new String(readLimited(input), StandardCharsets.UTF_8)); }
            if (status == 401 || (status == 403 && "environment_unavailable".equals(envelope.optJSONObject("error") == null ? "" : envelope.getJSONObject("error").optString("code")))) {
                reject(refresh); throw new SecurityException("session_revoked");
            }
            if (status != 200) throw new IOException("refresh_unavailable");
            JSONObject tokens = envelope.getJSONObject("data").getJSONObject("tokens");
            accept(tokens, generation.get(), false);
            return new JSONObject(tokens.toString());
        } finally { connection.disconnect(); }
    }
    /** 通知接口仅需小型JSON，限制响应体，避免异常服务耗尽后台内存。 */
    static byte[] readLimited(InputStream input) throws IOException {
        if (input == null) throw new IOException("empty_response");
        ByteArrayOutputStream output = new ByteArrayOutputStream(); byte[] buffer = new byte[8192];
        for (int n; (n = input.read(buffer)) != -1;) { if (output.size() + n > 1024 * 1024) throw new IOException("response_too_large"); output.write(buffer, 0, n); }
        return output.toByteArray();
    }
    /** 只有上游明确拒绝当前刷新凭据才撤销；迟到旧请求不能清除新会话，网络故障不撤销。 */
    synchronized void reject(String refresh) throws Exception {
        JSONObject saved = read(); JSONObject session = saved == null ? null : saved.optJSONObject("session");
        if (session != null && !refresh.isEmpty() && refresh.equals(session.optString("refresh_token"))) logout();
    }
    /** 在共享Web脚本执行前恢复权威凭据；仅返回给带私有Cookie的本机页面，不经过日志或外网。 */
    synchronized String bootstrap() throws Exception {
        JSONObject saved = read();
        StringBuilder script = new StringBuilder("(() => {localStorage.removeItem('photo-nini.refresh-lock');localStorage.removeItem('photo-nini.space-switch');");
        if (saved != null && saved.optBoolean("loggedOut")) {
            script.append("localStorage.removeItem('photo-nini.refresh-token');localStorage.removeItem('photo-nini.session');");
        } else if (saved != null && saved.optJSONObject("session") != null) {
            JSONObject session = saved.getJSONObject("session");
            script.append("localStorage.setItem('photo-nini.session',").append(quote(session.toString())).append(");localStorage.setItem('photo-nini.refresh-token',").append(quote(session.getString("refresh_token"))).append(");");
        }
        return script.append("})();").toString();
    }
    private static String quote(String value) { return JSONObject.quote(value).replace("<","\\u003c").replace("\u2028","\\u2028").replace("\u2029","\\u2029"); }
    private SecretKey key() throws Exception {
        KeyStore store = KeyStore.getInstance("AndroidKeyStore"); store.load(null);
        if (!store.containsAlias(alias)) {
            KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES,"AndroidKeyStore");
            generator.init(new KeyGenParameterSpec.Builder(alias,KeyProperties.PURPOSE_ENCRYPT|KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).setKeySize(256).build()); generator.generateKey();
        }
        return (SecretKey)store.getKey(alias,null);
    }
    private JSONObject read() throws Exception {
        try (InputStream input = file.openRead()) {
            JSONObject envelope = new JSONObject(new String(BundleIO.read(input),StandardCharsets.UTF_8));
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.DECRYPT_MODE,key(),new GCMParameterSpec(128,Base64.decode(envelope.getString("iv"),Base64.NO_WRAP)));
            cipher.updateAAD(apiBase.getBytes(StandardCharsets.UTF_8));
            return new JSONObject(new String(cipher.doFinal(Base64.decode(envelope.getString("data"),Base64.NO_WRAP)),StandardCharsets.UTF_8));
        } catch (FileNotFoundException absent) { return null;
        } catch (java.security.GeneralSecurityException | org.json.JSONException invalid) {
            // 密钥失效、配置切换或损坏时不恢复错误账户；保留加密文件供后续成功登录覆盖。
            return null;
        }
    }
    private void write(JSONObject value) throws Exception {
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding"); cipher.init(Cipher.ENCRYPT_MODE,key()); cipher.updateAAD(apiBase.getBytes(StandardCharsets.UTF_8));
        byte[] encrypted = cipher.doFinal(value.toString().getBytes(StandardCharsets.UTF_8));
        JSONObject envelope = new JSONObject().put("iv",Base64.encodeToString(cipher.getIV(),Base64.NO_WRAP)).put("data",Base64.encodeToString(encrypted,Base64.NO_WRAP));
        FileOutputStream output = file.startWrite();
        try { output.write(envelope.toString().getBytes(StandardCharsets.UTF_8)); file.finishWrite(output); }
        catch (Exception error) { file.failWrite(output); throw error; }
    }
}
