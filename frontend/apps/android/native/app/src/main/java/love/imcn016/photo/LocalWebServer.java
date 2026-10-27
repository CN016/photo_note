package love.imcn016.photo;

import android.content.Context;
import org.json.JSONObject;
import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.concurrent.*;

/** 仅监听127.0.0.1的私有资源服务；固定来源保留登录态，随机HttpOnly凭据阻止其他应用读取页面或代理API。 */
public final class LocalWebServer implements AutoCloseable {
    private final Context context;
    private final WebBundleUpdater bundles;
    private final ServerSocket listener;
    private final ExecutorService workers = new ThreadPoolExecutor(4, 4, 0, TimeUnit.SECONDS, new ArrayBlockingQueue<>(32));
    private final String token;
    private final String origin;
    private final SessionVault vault;
    private volatile String migrationScript = "";
    public LocalWebServer(Context context, WebBundleUpdater bundles) throws Exception {
        this.context = context; this.bundles = bundles;
        vault = new SessionVault(context,bundles.config.getString("apiOrigin"));
        var preferences = context.getSharedPreferences("web-local-origin", 0);
        token = preferences.getString("token", UUID.randomUUID().toString());
        int port = preferences.getInt("port", 0);
        // 端口被占用时拒绝启动，不能静默切换来源导致用户看似丢失登录态。
        listener = new ServerSocket(); listener.setReuseAddress(true);
        listener.bind(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), port), 32);
        origin = "http://127.0.0.1:" + listener.getLocalPort();
        if (!preferences.edit().putInt("port", listener.getLocalPort()).putString("token", token).commit()) throw new IOException("state");
        Thread accept = new Thread(() -> {
            while (!listener.isClosed()) try {
                Socket socket = listener.accept();
                try { workers.execute(() -> serve(socket)); } catch (RejectedExecutionException busy) { socket.close(); }
            } catch (IOException stopped) { break; }
        }, "web-loopback"); accept.setDaemon(true); accept.start();
    }
    /** 仅接收本APK旧扩展页的内存交接数据，不持久化额外令牌副本。 */
    public void migrate(org.json.JSONObject values) {
        org.json.JSONObject filtered = new org.json.JSONObject();
        try {
            for (Iterator<String> keys = values.keys(); keys.hasNext();) {
                String key = keys.next();
                if ((key.startsWith("photo-nini.") || key.startsWith("photo-nini-")) && values.optString(key).length() <= 65536) filtered.put(key, values.optString(key));
            }
        } catch (Exception ignored) { }
        migrationScript = "(() => {const values=" + filtered.toString().replace("<", "\\u003c").replace("\u2028", "\\u2028").replace("\u2029", "\\u2029") + ";for(const key of Object.keys(values)){if(localStorage.getItem(key)===null)localStorage.setItem(key,values[key]);}})();";
    }
    public void finishMigration() { migrationScript = ""; }
    public String origin() { return origin; }
    public String startUrl() { return origin + "/__start"; }
    /** 凭据只放原生请求头，不放URL，避免浏览器导航日志和历史记录保存秘密。 */
    public Map<String,String> startHeaders() { return Collections.singletonMap("X-Photo-Nini-Bootstrap", token); }
    /** 原生导航和消息共同使用精确来源校验，不采用字符串前缀匹配主机。 */
    public boolean trusted(String value) {
        try { URI uri = URI.create(value); return "http".equals(uri.getScheme()) && "127.0.0.1".equals(uri.getHost()) && uri.getPort() == listener.getLocalPort() && uri.getUserInfo() == null; }
        catch (Exception error) { return false; }
    }
    private void serve(Socket socket) {
        try (socket) {
            socket.setSoTimeout(30000);
            InputStream input = new BufferedInputStream(socket.getInputStream());
            OutputStream output = new BufferedOutputStream(socket.getOutputStream());
            String[] request = line(input).split(" ");
            if (request.length != 3 || !request[1].startsWith("/") || request[1].startsWith("//")) { status(output, 400); return; }
            Map<String,String> headers = new HashMap<>(); int length = 0;
            for (String value; !(value = line(input)).isEmpty();) {
                length += value.length(); if (length > 32768) throw new IOException("headers");
                int colon = value.indexOf(':'); if (colon < 1) throw new IOException("header");
                String name = value.substring(0,colon).toLowerCase(Locale.ROOT);
                if (headers.put(name, value.substring(colon+1).trim()) != null) throw new IOException("duplicate_header");
            }
            if (!headers.getOrDefault("host", "").equals("127.0.0.1:" + listener.getLocalPort())) { status(output, 403); return; }
            String source = headers.get("origin");
            if (source != null && !origin.equals(source)) { status(output, 403); return; }
            if (request[1].equals("/__start") && request[0].equals("GET") && token.equals(headers.get("x-photo-nini-bootstrap"))) {
                output.write(("HTTP/1.1 302 Found\r\nLocation: /\r\nSet-Cookie: photo_nini_local=" + token + "; HttpOnly; SameSite=Strict; Path=/\r\nContent-Length: 0\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n").getBytes(StandardCharsets.US_ASCII)); output.flush(); return;
            }
            boolean authorized = Arrays.stream(headers.getOrDefault("cookie", "").split(";")).anyMatch(value -> value.trim().equals("photo_nini_local=" + token));
            if (!authorized) { status(output, 403); return; }
            URI uri = URI.create(request[1]); String path = uri.getPath();
            if (path.startsWith("/api/v1/") || path.startsWith("/api/v2/") || path.equals("/healthz")) {
                proxy(request, headers, input, output); return;
            }
            if (!request[0].equals("GET") && !request[0].equals("HEAD")) { status(output, 405); return; }
            String resource = path.equals("/") ? "index.html" : path.substring(1);
            if (!WebBundleUpdater.safePath(resource)) { status(output, 404); return; }
            InputStream file;
            if (resource.equals("__session.js")) file = new ByteArrayInputStream(vault.bootstrap().getBytes(StandardCharsets.UTF_8));
            else if (resource.equals("__migration.js")) file = new ByteArrayInputStream(migrationScript.getBytes(StandardCharsets.UTF_8));
            else if (resource.equals("__native.js")) file = context.getAssets().open("native/page-bridge.js");
            else try { file = bundles.open(resource); }
            catch (FileNotFoundException missing) {
                // 只有明确的HTML导航才允许SPA回退；脚本、图片缺失必须返回404。
                if (resource.contains(".") || !headers.getOrDefault("accept", "").contains("text/html")) { status(output, 404); return; }
                resource = "index.html"; file = bundles.open(resource);
            }
            try (InputStream body = file) {
                String type = mime(resource);
                String policy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; media-src 'self' blob:; worker-src 'self' blob:; font-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'";
                output.write(("HTTP/1.1 200 OK\r\nContent-Type: " + type + "\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff\r\nReferrer-Policy: no-referrer\r\nContent-Security-Policy: " + policy + "\r\nConnection: close\r\n\r\n").getBytes(StandardCharsets.US_ASCII));
                if (!request[0].equals("HEAD")) {
                    if (resource.equals("index.html")) {
                        String html = new String(BundleIO.read(body), StandardCharsets.UTF_8);
                        output.write(html.replace("<head>", "<head><script src=\"/__migration.js\"></script><script src=\"/__session.js\"></script><script src=\"/__native.js\"></script>").getBytes(StandardCharsets.UTF_8));
                    } else BundleIO.copy(body, output);
                }
                output.flush();
            }
        } catch (Exception error) { /* 断开、格式错误和超时仅结束当前连接，绝不记录令牌或正文。 */ }
    }
    /** API只代理APK固定的HTTPS后端，保留流式上传下载，不接受客户端指定目标或重定向。 */
    private void proxy(String[] request, Map<String,String> headers, InputStream input, OutputStream output) throws Exception {
        if (!Arrays.asList("GET","POST","PUT","PATCH","DELETE","HEAD").contains(request[0]) || headers.containsKey("transfer-encoding")) { status(output,400); return; }
        long bytes = Long.parseLong(headers.getOrDefault("content-length", "0"));
        if (bytes < 0 || bytes > 128L * 1024 * 1024) { status(output,413); return; }
        long sessionGeneration = vault.generation();
        String endpoint = URI.create(request[1]).getPath();
        boolean sessionResponse = request[0].equals("POST") && Arrays.asList("/api/v1/auth/login","/api/v1/auth/register","/api/v1/auth/refresh","/api/v1/auth/switch-environment").contains(endpoint);
        boolean refreshing = endpoint.equals("/api/v1/auth/refresh") && request[0].equals("POST");
        ByteArrayOutputStream refreshBody = refreshing && bytes <= 16384 ? new ByteArrayOutputStream() : null;
        if (endpoint.equals("/api/v1/auth/logout") && request[0].equals("POST")) vault.logout();
        URL url = new URL(bundles.config.getString("apiOrigin") + request[1]);
        HttpURLConnection remote = (HttpURLConnection) url.openConnection();
        remote.setInstanceFollowRedirects(false); remote.setConnectTimeout(10000); remote.setReadTimeout(60000); remote.setRequestMethod(request[0]);
        for (String name : Arrays.asList("authorization","content-type","accept","range","if-none-match","if-modified-since","x-request-id")) if (headers.containsKey(name)) remote.setRequestProperty(name, headers.get(name));
        // 使用identity便于透传；不复制本机cookie、Origin或任意客户端转发头。
        remote.setRequestProperty("Accept-Encoding", "identity");
        try {
            if (bytes > 0) {
                remote.setDoOutput(true); remote.setFixedLengthStreamingMode(bytes);
                try (OutputStream body = remote.getOutputStream()) {
                    byte[] buffer = new byte[65536]; long remaining = bytes;
                    while (remaining > 0) { int n = input.read(buffer,0,(int)Math.min(buffer.length,remaining)); if (n < 0) throw new EOFException(); body.write(buffer,0,n); if (refreshBody != null) refreshBody.write(buffer,0,n); remaining -= n; }
                }
            }
            int code = remote.getResponseCode();
            if (code >= 300 && code < 400 && code != 304) { status(output,502); return; }
            byte[] sessionBytes = null;
            if (sessionResponse) {
                InputStream source = code >= 400 ? remote.getErrorStream() : remote.getInputStream();
                if (source != null) try (source; ByteArrayOutputStream body = new ByteArrayOutputStream()) {
                    byte[] buffer = new byte[8192];
                    for (int n; (n = source.read(buffer)) != -1;) { if (body.size()+n > 1024*1024) throw new IOException("session_response_size"); body.write(buffer,0,n); }
                    sessionBytes = body.toByteArray();
                }
                if (sessionBytes != null && code >= 200 && code < 300) {
                    JSONObject envelope = new JSONObject(new String(sessionBytes,StandardCharsets.UTF_8));
                    JSONObject data = envelope.optJSONObject("data");
                    if (data != null && data.optJSONObject("tokens") != null) vault.accept(data.getJSONObject("tokens"),sessionGeneration,endpoint.equals("/api/v1/auth/login") || endpoint.equals("/api/v1/auth/register"));
                }
                boolean revokedEnvironment = false;
                if (code == 403 && sessionBytes != null) try {
                    JSONObject failure = new JSONObject(new String(sessionBytes,StandardCharsets.UTF_8)).optJSONObject("error");
                    revokedEnvironment = failure != null && "environment_unavailable".equals(failure.optString("code"));
                } catch (org.json.JSONException ignored) { /* 代理HTML或未知403不能被当作会话撤销。 */ }
                if (refreshing && (code == 401 || revokedEnvironment) && refreshBody != null) {
                    JSONObject submitted = new JSONObject(refreshBody.toString("UTF-8")); vault.reject(submitted.optString("refresh_token"));
                }
            }
            StringBuilder response = new StringBuilder("HTTP/1.1 " + code + " Response\r\nConnection: close\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff\r\n");
            for (String name : Arrays.asList("Content-Type","Content-Length","Content-Disposition","Content-Range","Accept-Ranges","ETag","Last-Modified")) {
                String value = remote.getHeaderField(name);
                if (value != null && value.indexOf('\r') < 0 && value.indexOf('\n') < 0) response.append(name).append(": ").append(value).append("\r\n");
            }
            output.write(response.append("\r\n").toString().getBytes(StandardCharsets.ISO_8859_1));
            if (sessionBytes != null) output.write(sessionBytes);
            else if (!sessionResponse) {
                InputStream body = code >= 400 ? remote.getErrorStream() : remote.getInputStream();
                if (body != null) try (body) { if (!request[0].equals("HEAD")) BundleIO.copy(body, output); }
            }
            output.flush();
        } finally { remote.disconnect(); }
    }
    private static String line(InputStream input) throws IOException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        for (int c; (c = input.read()) != -1;) { if (c == '\n') return new String(bytes.toByteArray(), StandardCharsets.US_ASCII).replace("\r", ""); if (bytes.size() > 8192) throw new IOException("line"); bytes.write(c); }
        throw new EOFException();
    }
    private static String mime(String path) {
        if (path.endsWith(".html")) return "text/html; charset=utf-8";
        if (path.endsWith(".js") || path.endsWith(".mjs")) return "text/javascript; charset=utf-8";
        if (path.endsWith(".css")) return "text/css; charset=utf-8";
        if (path.endsWith(".wasm")) return "application/wasm";
        if (path.endsWith(".json")) return "application/json";
        if (path.endsWith(".svg")) return "image/svg+xml";
        String type = URLConnection.guessContentTypeFromName(path); return type == null ? "application/octet-stream" : type;
    }
    private static void status(OutputStream output, int code) throws IOException { output.write(("HTTP/1.1 " + code + " Response\r\nContent-Length: 0\r\nConnection: close\r\n\r\n").getBytes(StandardCharsets.US_ASCII)); output.flush(); }
    public void close() { vault.stop(); try { listener.close(); } catch (IOException ignored) { } workers.shutdownNow(); }
}
