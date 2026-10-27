package love.imcn016.photo;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Base64;
import org.json.JSONObject;
import java.io.*;
import java.net.*;
import java.nio.file.Files;
import java.security.*;
import java.security.spec.X509EncodedKeySpec;
import java.util.*;
import java.util.zip.*;

/** 仅接受固定发布源及APK内置公钥签名的资源包；候选版本未启动成功前不覆盖稳定版本。 */
public final class WebBundleUpdater {
    public interface Progress { void show(String phase, int percent); }
    private final Context context;
    private final SharedPreferences state;
    private final File root;
    final JSONObject config;
    private String selected;
    private String candidate;
    public volatile String displayVersion = "";
    private final Set<String> allowed = new HashSet<>();
    public WebBundleUpdater(Context context) throws Exception {
        this(context, readConfig(context));
    }
    /** 测试可注入独立配置和隔离Context；生产入口只读取APK固定资源。 */
    WebBundleUpdater(Context context, JSONObject config) throws Exception {
        this.context = context;
        state = context.getSharedPreferences("web-updates-v1", 0);
        root = new File(context.getFilesDir(), "web-bundles");
        if (!root.exists() && !root.mkdirs()) throw new IOException("storage");
        this.config = config;
    }
    private static JSONObject readConfig(Context context) throws Exception {
        try (InputStream input = context.getAssets().open("update-config.json")) {
            return new JSONObject(new String(BundleIO.read(input), java.nio.charset.StandardCharsets.UTF_8));
        }
    }
    /** 崩溃/强制退出前尚未确认的版本加入拒绝列表，下一次启动退回稳定版本。 */
    @android.annotation.SuppressLint("ApplySharedPref") // 后台线程需要持久化完成后才允许启动候选，避免崩溃窗口。
    public synchronized void prepare(Progress progress) {
        displayVersion = config.optString("version");
        selected = state.getString("stable", null);
        String interrupted = state.getString("pending", null);
        if (interrupted != null) state.edit().putString("rejected", interrupted).remove("pending").commit();
        try {
            if (selected != null && Long.parseLong(selected) <= config.getLong("release")) { selected = null; state.edit().remove("stable").commit(); }
            if (selected != null) verifyLocal(selected);
        } catch (Exception invalid) { selected = null; state.edit().remove("stable").commit(); }
        if (!config.optBoolean("enabled")) return;
        try {
            progress.show("checking", 0);
            byte[] envelope = download(new URL(config.getString("url")), 1024 * 1024, null);
            JSONObject release = verifyEnvelope(envelope);
            String id = Long.toString(release.getLong("release"));
            long baseline = config.getLong("release");
            long stable = selected == null ? baseline : Math.max(baseline, Long.parseLong(selected));
            if (release.getLong("release") <= stable || id.equals(state.getString("rejected", null))) return;
            if (release.getInt("minShellCode") > config.getInt("shellCode") || release.getInt("bridgeVersion") != 1) return;
            if (!config.getString("apiOrigin").equals(release.getString("apiOrigin"))) throw new SecurityException("api_origin");
            URL url = new URL(release.getString("url"));
            URL feed = new URL(config.getString("url"));
            if (!origin(url).equals(origin(feed))) throw new SecurityException("package_origin");
            long bytes = release.getLong("bytes");
            if (bytes <= 0 || bytes > 128L * 1024 * 1024) throw new IOException("size");
            displayVersion = release.optString("version");
            progress.show("downloading", 0);
            File archive = downloadArchive(url, bytes, release.getString("sha256"), progress);
            progress.show("verifying", 100);
            File directory = new File(root, id);
            remove(directory);
            if (!directory.mkdir()) throw new IOException("directory");
            try {
                JSONObject files = release.getJSONObject("files");
                if (files.length() > 4096 || !files.has("index.html")) throw new SecurityException("files");
                Set<String> extracted = new HashSet<>();
                long total = 0;
                try (ZipInputStream zip = new ZipInputStream(new FileInputStream(archive))) {
                    int entries = 0;
                    for (ZipEntry entry; (entry = zip.getNextEntry()) != null;) {
                        if (++entries > 8192) throw new SecurityException("entries");
                        String name = entry.getName();
                        if (entry.isDirectory()) continue;
                        if (!safePath(name) || !files.has(name) || !extracted.add(name)) throw new SecurityException("path");
                        File output = new File(directory, name);
                        if (!output.getCanonicalPath().startsWith(directory.getCanonicalPath() + File.separator)) throw new SecurityException("path");
                        File parent = output.getParentFile();
                        if (!parent.exists() && !parent.mkdirs()) throw new IOException("directory");
                        MessageDigest hash = MessageDigest.getInstance("SHA-256");
                        try (OutputStream stream = new FileOutputStream(output)) {
                            byte[] buffer = new byte[65536];
                            for (int n; (n = zip.read(buffer)) != -1;) {
                                total += n;
                                if (total > 256L * 1024 * 1024) throw new IOException("expanded_size");
                                stream.write(buffer, 0, n); hash.update(buffer, 0, n);
                            }
                        }
                        if (!hex(hash.digest()).equals(files.getString(name))) throw new SecurityException("file_digest");
                    }
                }
                if (extracted.size() != files.length()) throw new SecurityException("missing_file");
                Files.write(new File(directory, ".release.json").toPath(), envelope);
                candidate = id; selected = id;
                if (!state.edit().putString("pending", id).commit()) throw new IOException("state");
            } catch (Exception error) { remove(directory); throw error; }
            finally { archive.delete(); }
        } catch (Exception error) {
            // 网络、签名或资源错误只影响此次更新，不把远端正文/地址写入日志。
            candidate = null; selected = state.getString("stable", null);
            progress.show("fallback", 0);
        }
        try { if (selected != null) verifyLocal(selected); } catch (Exception error) { selected = null; }
    }
    /** 重新校验持久化副本，损坏文件不得因曾被下载过而获得信任。 */
    private void verifyLocal(String id) throws Exception {
        if (!id.matches("[0-9]{1,16}")) throw new SecurityException("id");
        File directory = new File(root, id);
        JSONObject release = verifyEnvelope(Files.readAllBytes(new File(directory, ".release.json").toPath()));
        if (release.getInt("minShellCode") > config.getInt("shellCode") || release.getInt("bridgeVersion") != 1 || !config.getString("apiOrigin").equals(release.getString("apiOrigin"))) throw new SecurityException("compatibility");
        if (!id.equals(Long.toString(release.getLong("release")))) throw new SecurityException("id");
        JSONObject files = release.getJSONObject("files");
        allowed.clear();
        for (Iterator<String> names = files.keys(); names.hasNext();) {
            String name = names.next();
            if (!safePath(name)) throw new SecurityException("path");
            try (InputStream input = new FileInputStream(new File(directory, name))) {
                MessageDigest hash = MessageDigest.getInstance("SHA-256"); byte[] buffer = new byte[65536];
                for (int n; (n = input.read(buffer)) != -1;) hash.update(buffer, 0, n);
                if (!hex(hash.digest()).equals(files.getString(name))) throw new SecurityException("file_digest");
            }
            allowed.add(name);
        }
    }
    /** 只有实际共享应用完成挂载并建立返回键连接后才提交稳定版本。 */
    public synchronized void confirm() {
        if (candidate != null && candidate.equals(selected)) {
            if (state.edit().putString("stable", candidate).remove("pending").commit()) candidate = null;
        }
        // 保留稳定版和本次候选；清理过期包避免长时间更新耗尽存储。
        File[] entries = root.listFiles();
        if (entries != null) for (File entry : entries) if (!entry.getName().equals(selected)) remove(entry);
    }
    public synchronized boolean hasCandidate() { return candidate != null; }
    @android.annotation.SuppressLint("ApplySharedPref") // 后台线程先持久化拒绝状态，再切换显示资源。
    public synchronized boolean rollback() {
        if (candidate == null) return false;
        state.edit().putString("rejected", candidate).remove("pending").commit();
        candidate = null; selected = state.getString("stable", null);
        try { if (selected != null) verifyLocal(selected); } catch (Exception error) { selected = null; }
        return true;
    }
    /** 仅从签名清单内读取文件；找不到时由资源服务器判断是否SPA导航。 */
    public synchronized InputStream open(String path) throws IOException {
        if (!safePath(path)) throw new FileNotFoundException();
        if (selected == null) return context.getAssets().open("browser/" + path);
        if (!allowed.contains(path)) throw new FileNotFoundException();
        return new FileInputStream(new File(new File(root, selected), path));
    }
    public static boolean safePath(String name) {
        if (name.isEmpty() || name.length() > 512 || name.startsWith("/") || name.contains("\\") || name.indexOf(':') >= 0 || name.indexOf('\0') >= 0) return false;
        for (String part : name.split("/", -1)) if (part.isEmpty() || part.equals(".") || part.equals("..")) return false;
        return !name.startsWith(".");
    }
    private JSONObject verifyEnvelope(byte[] envelope) throws Exception {
        JSONObject json = new JSONObject(new String(envelope, java.nio.charset.StandardCharsets.UTF_8));
        byte[] payload = Base64.decode(json.getString("payload"), Base64.DEFAULT);
        PublicKey key = KeyFactory.getInstance("RSA").generatePublic(new X509EncodedKeySpec(Base64.decode(config.getString("publicKey"), Base64.DEFAULT)));
        Signature check = Signature.getInstance("SHA256withRSA"); check.initVerify(key); check.update(payload);
        if (!check.verify(Base64.decode(json.getString("signature"), Base64.DEFAULT))) throw new SecurityException("signature");
        JSONObject release = new JSONObject(new String(payload, java.nio.charset.StandardCharsets.UTF_8));
        if (release.getInt("schema") != 1 || release.getLong("release") <= 0 || release.getLong("release") > 9007199254740991L) throw new SecurityException("schema");
        return release;
    }
    /** ZIP下载直接落私有临时文件并流式哈希，避免大包在Java堆中保留多份副本。 */
    private File downloadArchive(URL url, long expected, String digest, Progress progress) throws Exception {
        if (!"https".equals(url.getProtocol()) || url.getUserInfo() != null || url.getRef() != null) throw new SecurityException("url");
        File archive = new File(root, "download.partial");
        HttpURLConnection connection = (HttpURLConnection) url.openConnection();
        connection.setInstanceFollowRedirects(false); connection.setConnectTimeout(4000); connection.setReadTimeout(15000);
        try {
            if (connection.getResponseCode() != 200 || connection.getContentLengthLong() > expected) throw new IOException("http");
            long count = 0; int last = -1; MessageDigest hash = MessageDigest.getInstance("SHA-256");
            try (InputStream input = connection.getInputStream(); OutputStream output = new FileOutputStream(archive)) {
                byte[] buffer = new byte[65536];
                for (int n; (n = input.read(buffer)) != -1;) {
                    count += n; if (count > expected || Thread.currentThread().isInterrupted()) throw new IOException("size");
                    output.write(buffer, 0, n); hash.update(buffer, 0, n);
                    int percent = (int)(100L * count / expected);
                    if (percent != last) { progress.show("downloading", percent); last = percent; }
                }
            }
            if (count != expected || !hex(hash.digest()).equals(digest)) throw new SecurityException("digest");
            return archive;
        } catch (Exception error) { archive.delete(); throw error; }
        finally { connection.disconnect(); }
    }
    /** 发布源固定HTTPS、禁止重定向，限制超时与实际读取量，防止无限下载。 */
    private byte[] download(URL url, int limit, Progress progress) throws Exception {
        if (!"https".equals(url.getProtocol()) || url.getUserInfo() != null || url.getRef() != null) throw new SecurityException("url");
        HttpURLConnection connection = (HttpURLConnection) url.openConnection();
        connection.setInstanceFollowRedirects(false); connection.setConnectTimeout(4000); connection.setReadTimeout(15000);
        connection.setRequestProperty("Cache-Control", "no-cache");
        try {
            if (connection.getResponseCode() != 200) throw new IOException("http");
            if (connection.getContentLengthLong() > limit) throw new IOException("size");
            try (InputStream input = connection.getInputStream(); ByteArrayOutputStream output = new ByteArrayOutputStream()) {
                byte[] buffer = new byte[65536]; int count = 0, last = -1;
                for (int n; (n = input.read(buffer)) != -1;) {
                    count += n; if (count > limit) throw new IOException("size"); output.write(buffer, 0, n);
                    int percent = (int)(100L * count / limit);
                    if (progress != null && percent != last) { progress.show("downloading", percent); last = percent; }
                }
                return output.toByteArray();
            }
        } finally { connection.disconnect(); }
    }
    private static String origin(URL url) { return url.getProtocol() + "://" + url.getAuthority(); }
    private static String hex(byte[] bytes) { StringBuilder out = new StringBuilder(); for (byte b : bytes) out.append(String.format(java.util.Locale.ROOT, "%02x", b & 255)); return out.toString(); }
    /** 只清理应用私有资源包目录，递归过程中拒绝跟随符号链接。 */
    private static void remove(File file) {
        if (Files.isSymbolicLink(file.toPath())) { file.delete(); return; }
        File[] children = file.listFiles(); if (children != null) for (File child : children) remove(child);
        file.delete();
    }
}
