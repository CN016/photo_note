package love.imcn016.photo;

import static org.junit.Assert.*;
import android.content.Context;
import android.content.ContextWrapper;
import android.content.SharedPreferences;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.Base64;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.junit.*;
import org.junit.runner.RunWith;
import org.json.JSONObject;
import javax.net.ssl.*;
import javax.security.auth.x500.X500Principal;
import java.io.*;
import java.net.*;
import java.security.*;
import java.security.cert.X509Certificate;
import java.util.*;
import java.util.concurrent.*;
import java.util.zip.*;

/** 仅在隔离模拟器上用即时测试密钥和本机TLS服务器验收，不访问真实用户文件或生产发布源。 */
@RunWith(AndroidJUnit4.class)
public class WebUpdateInstrumentedTest {
    private Context isolated;
    private SSLServerSocket tls;
    private KeyStore keys;
    private PrivateKey privateKey;
    private X509Certificate certificate;
    private SSLSocketFactory previousFactory;
    private HostnameVerifier previousVerifier;
    private volatile byte[] feed;
    private volatile byte[] archive;
    private String base;
    private String alias;
    private volatile String lastApiPath = "";
    private volatile String lastApiBody = "";
    private volatile String lastApiAuthorization = "";
    private volatile String lastApiCookie = "";
    private final ExecutorService server = Executors.newCachedThreadPool();
    @Before public void prepare() throws Exception {
        Context app = InstrumentationRegistry.getInstrumentation().getTargetContext();
        String prefix = "update-fixture-" + UUID.randomUUID();
        File directory = new File(app.getCacheDir(), prefix); assertTrue(directory.mkdir());
        isolated = new ContextWrapper(app) {
            @Override public File getFilesDir() { return directory; }
            @Override public SharedPreferences getSharedPreferences(String name, int mode) { return app.getSharedPreferences(prefix + name, mode); }
        };
        alias = prefix;
        KeyPairGenerator generator = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_RSA, "AndroidKeyStore");
        generator.initialize(new KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN | KeyProperties.PURPOSE_VERIFY)
            .setKeySize(2048).setDigests(KeyProperties.DIGEST_NONE, KeyProperties.DIGEST_SHA256, KeyProperties.DIGEST_SHA384, KeyProperties.DIGEST_SHA512).setSignaturePaddings(KeyProperties.SIGNATURE_PADDING_RSA_PKCS1, KeyProperties.SIGNATURE_PADDING_RSA_PSS)
            .setCertificateSubject(new X500Principal("CN=127.0.0.1")).build());
        generator.generateKeyPair(); keys = KeyStore.getInstance("AndroidKeyStore"); keys.load(null);
        privateKey = (PrivateKey)keys.getKey(alias, null); certificate = (X509Certificate)keys.getCertificate(alias);
        KeyManagerFactory managers = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm()); managers.init(keys, null);
        KeyManager[] keyManagers = { new X509ExtendedKeyManager() {
            public String[] getClientAliases(String type, java.security.Principal[] issuers) { return new String[]{alias}; }
            public String chooseClientAlias(String[] types, java.security.Principal[] issuers, Socket socket) { return alias; }
            public String[] getServerAliases(String type, java.security.Principal[] issuers) { return new String[]{alias}; }
            public String chooseServerAlias(String type, java.security.Principal[] issuers, Socket socket) { return type.equals("RSA") ? alias : null; }
            public X509Certificate[] getCertificateChain(String selected) { return new X509Certificate[]{certificate}; }
            public PrivateKey getPrivateKey(String selected) { return privateKey; }
        }};
        SSLContext listenerContext = SSLContext.getInstance("TLS"); listenerContext.init(keyManagers, null, null);
        tls = (SSLServerSocket)listenerContext.getServerSocketFactory().createServerSocket(0, 10, InetAddress.getByName("127.0.0.1"));
        tls.setEnabledProtocols(new String[]{"TLSv1.2"});
        tls.setEnabledCipherSuites(new String[]{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"});
        base = "https://127.0.0.1:" + tls.getLocalPort();
        TrustManager[] trust = { new X509TrustManager() {
            public X509Certificate[] getAcceptedIssuers() { return new X509Certificate[]{certificate}; }
            public void checkClientTrusted(X509Certificate[] chain, String type) throws java.security.cert.CertificateException { checkServerTrusted(chain,type); }
            public void checkServerTrusted(X509Certificate[] chain, String type) throws java.security.cert.CertificateException {
                if (chain.length != 1 || !chain[0].equals(certificate)) throw new java.security.cert.CertificateException("fixture_only");
            }
        }};
        SSLContext client = SSLContext.getInstance("TLS"); client.init(null, trust, null);
        previousFactory = HttpsURLConnection.getDefaultSSLSocketFactory(); previousVerifier = HttpsURLConnection.getDefaultHostnameVerifier();
        HttpsURLConnection.setDefaultSSLSocketFactory(client.getSocketFactory());
        HttpsURLConnection.setDefaultHostnameVerifier((host, session) -> host.equals("127.0.0.1"));
        server.execute(() -> {
            while (!tls.isClosed()) try { Socket socket = tls.accept(); server.execute(() -> respond(socket)); } catch (IOException stopped) { break; }
        });
        release(2, false, false);
        HttpsURLConnection probe = (HttpsURLConnection)new URL(base+"/latest.json").openConnection();
        assertEquals("测试TLS服务器必须先建立可信连接",200,probe.getResponseCode()); probe.disconnect();
    }
    @After public void cleanup() throws Exception {
        if (previousFactory != null) HttpsURLConnection.setDefaultSSLSocketFactory(previousFactory);
        if (previousVerifier != null) HttpsURLConnection.setDefaultHostnameVerifier(previousVerifier);
        if (tls != null) tls.close(); server.shutdownNow();
        if (keys != null && alias != null) keys.deleteEntry(alias);
    }
    private void respond(Socket socket) {
        try (socket) {
            socket.setSoTimeout(5000);
            BufferedReader input = new BufferedReader(new InputStreamReader(socket.getInputStream()));
            String request = input.readLine(); if (request == null) return;
            Map<String,String> requestHeaders = new HashMap<>();
            for (String line; (line = input.readLine()) != null && !line.isEmpty();) {
                int colon = line.indexOf(':'); if (colon > 0) requestHeaders.put(line.substring(0,colon).toLowerCase(Locale.ROOT),line.substring(colon+1).trim());
            }
            if (request.contains("/api/")) {
                int remaining = Integer.parseInt(requestHeaders.getOrDefault("content-length","0")); StringBuilder body = new StringBuilder();
                while (remaining-- > 0) { int value = input.read(); if (value < 0) throw new EOFException(); body.append((char)value); }
                lastApiPath = request; lastApiBody = body.toString();
                lastApiAuthorization = requestHeaders.getOrDefault("authorization",""); lastApiCookie = requestHeaders.getOrDefault("cookie","");
            }
            byte[] bytes = request.contains("/latest.json ") ? feed : request.contains("/bundle.zip ") ? archive : "{\"ok\":true}".getBytes(java.nio.charset.StandardCharsets.UTF_8);
            OutputStream output = socket.getOutputStream();
            output.write(("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " + bytes.length + "\r\nConnection: close\r\n\r\n").getBytes(java.nio.charset.StandardCharsets.US_ASCII)); output.write(bytes); output.flush();
        } catch (Exception error) { android.util.Log.e("UpdateFixtureTLS", "fixture_server", error); }
    }
    private JSONObject config() throws Exception {
        return new JSONObject().put("enabled",true).put("release",1).put("version","fixture-1").put("shellCode",4).put("url",base+"/latest.json").put("apiOrigin",base+"/backendApi").put("publicKey",Base64.encodeToString(certificate.getPublicKey().getEncoded(),Base64.NO_WRAP));
    }
    private static String hash(byte[] bytes) throws Exception {
        StringBuilder out = new StringBuilder(); for (byte b : MessageDigest.getInstance("SHA-256").digest(bytes)) out.append(String.format(Locale.ROOT,"%02x",b&255)); return out.toString();
    }
    private void release(long number, boolean badHash, boolean traversal) throws Exception {
        byte[] html = ("<!doctype html><html><head></head><body>fixture-" + number + "</body></html>").getBytes(java.nio.charset.StandardCharsets.UTF_8);
        ByteArrayOutputStream buffer = new ByteArrayOutputStream();
        String name = traversal ? "../escape.html" : "index.html";
        try (ZipOutputStream zip = new ZipOutputStream(buffer)) { zip.putNextEntry(new ZipEntry(name)); zip.write(html); zip.closeEntry(); }
        archive = buffer.toByteArray();
        JSONObject files = new JSONObject().put(name,hash(html));
        if (traversal) files.put("index.html",hash(html));
        JSONObject release = new JSONObject().put("schema",1).put("release",number).put("version","fixture-"+number).put("minShellCode",4).put("bridgeVersion",1).put("apiOrigin",base+"/backendApi").put("url",base+"/bundle.zip").put("bytes",archive.length).put("sha256",badHash?"bad":hash(archive)).put("files",files);
        byte[] payload = release.toString().getBytes(java.nio.charset.StandardCharsets.UTF_8);
        Signature signature = Signature.getInstance("SHA256withRSA"); signature.initSign(privateKey); signature.update(payload);
        feed = new JSONObject().put("payload",Base64.encodeToString(payload,Base64.NO_WRAP)).put("signature",Base64.encodeToString(signature.sign(),Base64.NO_WRAP)).toString().getBytes(java.nio.charset.StandardCharsets.UTF_8);
    }
    private String page(WebBundleUpdater updater) throws Exception { try (InputStream input = updater.open("index.html")) { return new String(BundleIO.read(input),java.nio.charset.StandardCharsets.UTF_8); } }
    @Test public void signedInstallTamperRestartAndRollback() throws Exception {
        WebBundleUpdater updater = new WebBundleUpdater(isolated,config());
        List<String> phases = new ArrayList<>(); updater.prepare((phase, percent) -> phases.add(phase));
        assertTrue("签名包应真实下载并进入候选",page(updater).contains("fixture-2"));
        assertTrue(phases.contains("downloading")); assertTrue(phases.contains("verifying"));
        updater.confirm();
        release(3,true,false); updater = new WebBundleUpdater(isolated,config()); updater.prepare((p,n)->{});
        assertTrue("篡改包保留稳定版本",page(updater).contains("fixture-2"));
        release(3,false,false); updater.prepare((p,n)->{}); assertTrue(page(updater).contains("fixture-3"));
        updater = new WebBundleUpdater(isolated,config()); updater.prepare((p,n)->{});
        assertTrue("未确认版本在重启后回退",page(updater).contains("fixture-2"));
        release(4,false,true); updater.prepare((p,n)->{}); assertTrue(page(updater).contains("fixture-2"));
        assertFalse(new File(isolated.getFilesDir(),"escape.html").exists());
        release(5,false,false); updater.prepare((p,n)->{}); assertTrue(page(updater).contains("fixture-5"));
        assertTrue(updater.rollback()); assertTrue(page(updater).contains("fixture-2"));
        // 改签名本身同样必须失败，不能仅凭HTTPS接受包。
        JSONObject envelope = new JSONObject(new String(feed,java.nio.charset.StandardCharsets.UTF_8)); envelope.put("signature",Base64.encodeToString(new byte[256],Base64.NO_WRAP)); feed = envelope.toString().getBytes(java.nio.charset.StandardCharsets.UTF_8);
        updater.prepare((p,n)->{}); assertTrue(page(updater).contains("fixture-2"));
    }
    @Test public void localServerRejectsUnauthenticatedAndStreamsPinnedApi() throws Exception {
        WebBundleUpdater updater = new WebBundleUpdater(isolated,config()); updater.prepare((p,n)->{}); updater.confirm();
        try (LocalWebServer local = new LocalWebServer(isolated,updater)) {
            assertFalse(local.trusted(local.origin()+".attacker.example"));
            HttpURLConnection unauthorized = (HttpURLConnection)new URL(local.origin()+"/").openConnection();
            assertEquals(403,unauthorized.getResponseCode()); unauthorized.disconnect();
            String token = local.startHeaders().get("X-Photo-Nini-Bootstrap");
            HttpURLConnection bootstrap = (HttpURLConnection)new URL(local.startUrl()).openConnection();
            bootstrap.setInstanceFollowRedirects(false); bootstrap.setRequestProperty("X-Photo-Nini-Bootstrap",token);
            assertEquals(302,bootstrap.getResponseCode()); assertTrue(bootstrap.getHeaderField("Set-Cookie").contains("HttpOnly")); bootstrap.disconnect();
            HttpURLConnection page = (HttpURLConnection)new URL(local.origin()+"/").openConnection(); page.setRequestProperty("Cookie","photo_nini_local="+token);
            assertEquals(200,page.getResponseCode()); assertTrue(new String(BundleIO.read(page.getInputStream()),java.nio.charset.StandardCharsets.UTF_8).contains("/__native.js")); page.disconnect();
            HttpURLConnection missing = (HttpURLConnection)new URL(local.origin()+"/missing.js").openConnection(); missing.setRequestProperty("Cookie","photo_nini_local="+token); missing.setRequestProperty("Accept","text/html"); assertEquals(404,missing.getResponseCode()); missing.disconnect();
            HttpURLConnection api = (HttpURLConnection)new URL(local.origin()+"/api/v1/test").openConnection(); api.setRequestProperty("Cookie","photo_nini_local="+token);
            assertEquals(200,api.getResponseCode()); assertTrue(new String(BundleIO.read(api.getInputStream()),java.nio.charset.StandardCharsets.UTF_8).contains("ok")); api.disconnect();
            // 验证流式上传、鉴权头和既有反向代理前缀；本机凭据绝不能发往上游。
            HttpURLConnection post = (HttpURLConnection)new URL(local.origin()+"/api/v1/test").openConnection();
            post.setRequestMethod("POST"); post.setDoOutput(true); post.setFixedLengthStreamingMode(12);
            post.setRequestProperty("Cookie","photo_nini_local="+token); post.setRequestProperty("Authorization","Bearer fixture-token");
            try (OutputStream body = post.getOutputStream()) { body.write("fixture-body".getBytes(java.nio.charset.StandardCharsets.US_ASCII)); }
            assertEquals(200,post.getResponseCode()); post.disconnect();
            assertTrue(lastApiPath.contains("/backendApi/api/v1/test")); assertEquals("fixture-body",lastApiBody);
            assertEquals("Bearer fixture-token",lastApiAuthorization); assertEquals("",lastApiCookie);
            HttpURLConnection cross = (HttpURLConnection)new URL(local.origin()+"/api/v1/test").openConnection(); cross.setRequestProperty("Cookie","photo_nini_local="+token); cross.setRequestProperty("Origin","https://attacker.invalid"); assertEquals(403,cross.getResponseCode()); cross.disconnect();
        }
    }
}
