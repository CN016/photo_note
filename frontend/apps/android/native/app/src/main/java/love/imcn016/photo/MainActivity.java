package love.imcn016.photo;

import android.content.Intent;
import android.net.Uri;
import android.provider.OpenableColumns;
import androidx.activity.result.PickVisualMediaRequest;
import java.io.File;
import java.io.InputStream;
import java.io.FileOutputStream;
import android.os.Bundle;
import android.view.View;
import android.widget.FrameLayout;
import android.widget.ProgressBar;
import android.widget.TextView;
import androidx.activity.EdgeToEdge;
import androidx.activity.OnBackPressedCallback;
import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import androidx.appcompat.app.AppCompatActivity;
import androidx.core.graphics.Insets;
import androidx.core.splashscreen.SplashScreen;
import androidx.core.view.ViewCompat;
import androidx.core.view.WindowInsetsCompat;
import org.json.JSONObject;
import org.mozilla.geckoview.*;
import java.util.ArrayList;

/** 随APK分发Gecko内核；固定扩展只为本会话内可信的本机资源页面提供系统能力。 */
public class MainActivity extends AppCompatActivity {
    public static final String EXTENSION_ID = "photo-nini@imcn016.love";
    private static GeckoRuntime runtime;
    private GeckoSession session;
    private GeckoView browserView;
    private NativeShellService shell;
    private WebExtension.Port pagePort;
    private String extensionBase;
    private boolean canGoBack;
    private TextView status;
    private View updatePage;
    private TextView updateVersion;
    private android.widget.Button retryButton;
    private boolean rollbackPending;
    private ProgressBar updateProgress;
    private WebBundleUpdater updater;
    private LocalWebServer localWeb;
    private final android.os.Handler updateHandler = new android.os.Handler(android.os.Looper.getMainLooper());
    private final java.util.concurrent.ExecutorService updateIO = java.util.concurrent.Executors.newSingleThreadExecutor();
    private GeckoSession.PromptDelegate.FilePrompt filePrompt;
    private GeckoResult<GeckoSession.PromptDelegate.PromptResponse> fileResult;
    private ActivityResultLauncher<Intent> fileLauncher;

    /** 单进程共享内核运行时，Activity 重建不重复启动 Gecko 进程。 */
    public static synchronized GeckoRuntime getRuntime(android.content.Context context) {
        if (runtime == null) {
            GeckoRuntimeSettings settings = new GeckoRuntimeSettings.Builder()
                .remoteDebuggingEnabled(false).consoleOutput(false)
                .allowInsecureConnections(GeckoRuntimeSettings.ALLOW_ALL).build();
            runtime = GeckoRuntime.create(context.getApplicationContext(), settings);
        }
        return runtime;
    }
    public NativeShellService getShell() { return shell; }
    public GeckoSession getSession() { return session; }
    boolean hasNativePageConnection() { return pagePort != null; }

    @Override public void onCreate(Bundle state) {
        SplashScreen.installSplashScreen(this);
        super.onCreate(state);
        EdgeToEdge.enable(this);
        shell = new NativeShellService(this);
        clearExpiredPickedFiles();
        fileLauncher = registerForActivityResult(new ActivityResultContracts.StartActivityForResult(), result -> {
            if (filePrompt == null || fileResult == null) return;
            var prompt = filePrompt;
            var response = fileResult;
            filePrompt = null; fileResult = null;
            try {
                Intent data = result.getData();
                if (result.getResultCode() != RESULT_OK || data == null) { response.complete(prompt.dismiss()); return; }
                ArrayList<Uri> uris = new ArrayList<>();
                if (data.getClipData() != null) for (int i = 0; i < data.getClipData().getItemCount(); i++) uris.add(data.getClipData().getItemAt(i).getUri());
                else if (data.getData() != null) uris.add(data.getData());
                // Gecko 文件提示需要可直接读取的本地文件；不能把相册供应商的 content URI 当文件路径。
                // 在后台复制到本应用缓存，保持原文件名和扩展名，网页后续仍执行类型与大小校验。
                new Thread(() -> {
                    try {
                        ArrayList<Uri> local = new ArrayList<>();
                        for (Uri uri : uris) local.add(copyPickedFile(uri));
                        runOnUiThread(() -> response.complete(local.isEmpty() ? prompt.dismiss()
                            : prompt.type == GeckoSession.PromptDelegate.FilePrompt.Type.MULTIPLE
                                ? prompt.confirm(this, local.toArray(new Uri[0])) : prompt.confirm(this, local.get(0))));
                    } catch (Exception error) { runOnUiThread(() -> response.complete(prompt.dismiss())); }
                }, "picked-file-copy").start();
            } catch (Exception error) { response.complete(prompt.dismiss()); }
        });
        FrameLayout content = new FrameLayout(this);
        content.setBackgroundColor(android.graphics.Color.rgb(243,247,254));
        browserView = new GeckoView(this);
        content.addView(browserView, new FrameLayout.LayoutParams(-1, -1));
        status = new TextView(this);
        status.setText(R.string.engine_loading);
        status.setTextColor(android.graphics.Color.rgb(23,43,77));
        status.setGravity(android.view.Gravity.CENTER);
        android.widget.LinearLayout updating = new android.widget.LinearLayout(this);
        updating.setOrientation(android.widget.LinearLayout.VERTICAL);
        updating.setGravity(android.view.Gravity.CENTER);
        updating.setPadding(40, 40, 40, 40);
        boolean night = (getResources().getConfiguration().uiMode & android.content.res.Configuration.UI_MODE_NIGHT_MASK) == android.content.res.Configuration.UI_MODE_NIGHT_YES;
        updating.setBackgroundColor(night ? android.graphics.Color.rgb(16,19,24) : android.graphics.Color.rgb(243,247,254));
        status.setTextColor(night ? android.graphics.Color.WHITE : android.graphics.Color.rgb(23,43,77));
        status.setTextSize(18);
        updating.addView(status, new android.widget.LinearLayout.LayoutParams(-1, -2));
        updateVersion = new TextView(this); updateVersion.setGravity(android.view.Gravity.CENTER);
        updateVersion.setTextColor(night ? android.graphics.Color.LTGRAY : android.graphics.Color.DKGRAY);
        updating.addView(updateVersion, new android.widget.LinearLayout.LayoutParams(-1, -2));
        updateProgress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        updateProgress.setIndeterminate(true);
        var barLayout = new android.widget.LinearLayout.LayoutParams(-1, 24); barLayout.topMargin = 32;
        updating.addView(updateProgress, barLayout);
        retryButton = new android.widget.Button(this);
        retryButton.setText(R.string.update_retry);
        retryButton.setVisibility(View.GONE);
        retryButton.setOnClickListener(view -> { retryButton.setEnabled(false); recreate(); });
        updating.addView(retryButton);
        updatePage = updating;
        content.addView(updatePage, new FrameLayout.LayoutParams(-1, -1));
        setContentView(content);
        // 原生容器统一避让系统栏和输入法，不依赖老系统 WebView 的 safe-area 实现。
        ViewCompat.setOnApplyWindowInsetsListener(content, (view, insets) -> {
            Insets bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() | WindowInsetsCompat.Type.displayCutout());
            Insets ime = insets.getInsets(WindowInsetsCompat.Type.ime());
            view.setPadding(bars.left, bars.top, bars.right, Math.max(bars.bottom, ime.bottom));
            return WindowInsetsCompat.CONSUMED;
        });
        getOnBackPressedDispatcher().addCallback(this, new OnBackPressedCallback(true) {
            @Override public void handleOnBackPressed() {
                if (pagePort != null) {
                    try { pagePort.postMessage(new JSONObject().put("type", "back")); } catch (Exception ignored) { moveTaskToBack(true); }
                } else if (canGoBack) session.goBack(); else moveTaskToBack(true);
            }
        });
        updateIO.execute(() -> {
            try {
                updater = new WebBundleUpdater(getApplicationContext());
                updater.prepare((phase, percent) -> runOnUiThread(() -> {
                    if (isDestroyed()) return;
                    int label = switch (phase) { case "checking" -> R.string.update_checking; case "downloading" -> R.string.update_downloading; case "verifying" -> R.string.update_verifying; default -> R.string.update_fallback; };
                    status.setText(getString(label, percent));
                    updateVersion.setText(getString(R.string.update_version, updater.displayVersion));
                    updateProgress.setIndeterminate(!phase.equals("downloading")); updateProgress.setProgress(percent);
                }));
                if (Thread.currentThread().isInterrupted()) return;
                localWeb = new LocalWebServer(getApplicationContext(), updater);
                runOnUiThread(() -> {
                    if (isDestroyed()) { localWeb.close(); return; }
                    try { status.setText(R.string.engine_loading); openEngine(); }
                    catch (Exception error) { showEngineFailure(); }
                });
            } catch (Exception error) { runOnUiThread(this::showEngineFailure); }
        });
    }

    /** 导航、文件选择和消息边界在创建会话时装配，未验证的远端导航默认拒绝。 */
    private void openEngine() {
        GeckoRuntime engine = getRuntime(this);
        session = new GeckoSession();
        session.setNavigationDelegate(new GeckoSession.NavigationDelegate() {
            @Override public void onCanGoBack(GeckoSession current, boolean value) { canGoBack = value; }
            @Override public GeckoResult<AllowOrDeny> onLoadRequest(GeckoSession current, LoadRequest request) {
                if ((extensionBase != null && request.uri.startsWith(extensionBase)) || (localWeb != null && localWeb.trusted(request.uri))) return GeckoResult.fromValue(AllowOrDeny.ALLOW);
                openExternal(request.uri);
                return GeckoResult.fromValue(AllowOrDeny.DENY);
            }
            @Override public GeckoResult<GeckoSession> onNewSession(GeckoSession current, String uri) { openExternal(uri); return null; }
        });
        session.setProgressDelegate(new GeckoSession.ProgressDelegate() {
            @Override public void onPageStop(GeckoSession current, boolean success) {
                if (current != session || isDestroyed()) return;
                if (!success) { if (!rollbackUpdate()) showEngineFailure(); }
                // 页面HTTP成功不等于共享应用已挂载；等待受授权返回键连接确认。
            }
        });
        session.setContentDelegate(new GeckoSession.ContentDelegate() {
            @Override public void onCrash(GeckoSession current) { if (current == session && !rollbackUpdate()) showEngineFailure(); }
        });
        session.setPromptDelegate(new GeckoSession.PromptDelegate() {
            @Override public GeckoResult<PromptResponse> onFilePrompt(GeckoSession current, FilePrompt prompt) {
                if (fileResult != null || prompt.type == FilePrompt.Type.FOLDER) return GeckoResult.fromValue(prompt.dismiss());
                filePrompt = prompt; fileResult = new GeckoResult<>();
                GeckoResult<PromptResponse> result = fileResult;
                Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*")
                    .putExtra(Intent.EXTRA_ALLOW_MULTIPLE, prompt.type == FilePrompt.Type.MULTIPLE);
                if (prompt.mimeTypes != null) {
                    String[] types = java.util.Arrays.stream(prompt.mimeTypes).filter(type -> type.contains("/")).toArray(String[]::new);
                    if (types.length == 1) intent.setType(types[0]);
                    else if (types.length > 1) intent.putExtra(Intent.EXTRA_MIME_TYPES, types);
                }
                // 图片和视频输入采用 AndroidX 照片选择器：优先使用 Photo Picker，旧系统使用相册的 ACTION_PICK。
                // 混合附件输入仍保留文件管理器，避免 PDF 和 Office 被过滤。
                if (prompt.mimeTypes != null && prompt.mimeTypes.length > 0
                    && java.util.Arrays.stream(prompt.mimeTypes).allMatch(type -> type.startsWith("image/") || type.startsWith("video/"))) {
                    PickVisualMediaRequest request = new PickVisualMediaRequest.Builder()
                        .setMediaType(java.util.Arrays.stream(prompt.mimeTypes).allMatch(type -> type.startsWith("image/"))
                            ? ActivityResultContracts.PickVisualMedia.ImageOnly.INSTANCE
                            : ActivityResultContracts.PickVisualMedia.ImageAndVideo.INSTANCE).build();
                    intent = prompt.type == FilePrompt.Type.MULTIPLE
                        ? new ActivityResultContracts.PickMultipleVisualMedia().createIntent(MainActivity.this, request)
                        : new ActivityResultContracts.PickVisualMedia().createIntent(MainActivity.this, request);
                    // 未安装系统/回移照片选择器的 Android 9+ 使用相册 Intent，避免降级到文档管理器。
                    // 旧相册可能仅返回单个媒体，回调同时兼容单选和多选结果。
                    if (!ActivityResultContracts.PickVisualMedia.isPhotoPickerAvailable(MainActivity.this)) {
                        boolean imageOnly = java.util.Arrays.stream(prompt.mimeTypes).allMatch(type -> type.startsWith("image/"));
                        intent = new Intent(Intent.ACTION_PICK)
                            .setDataAndType(android.provider.MediaStore.Images.Media.EXTERNAL_CONTENT_URI, imageOnly ? "image/*" : "*/*")
                            .putExtra(Intent.EXTRA_MIME_TYPES, imageOnly ? new String[] { "image/*" } : new String[] { "image/*", "video/*" })
                            .putExtra(Intent.EXTRA_ALLOW_MULTIPLE, prompt.type == FilePrompt.Type.MULTIPLE);
                    }
                }
                try { fileLauncher.launch(intent); }
                catch (Exception error) { filePrompt = null; fileResult = null; result.complete(prompt.dismiss()); }
                return result;
            }
        });
        session.open(engine);
        browserView.setSession(session);
        engine.getWebExtensionController().ensureBuiltIn("resource://android/assets/browser/", EXTENSION_ID).accept(extension -> {
            if (isFinishing() || isDestroyed()) return;
            extensionBase = extension.metaData.baseUrl;
            WebExtension.MessageDelegate delegate = new WebExtension.MessageDelegate() {
                @Override public GeckoResult<Object> onMessage(String app, Object message, WebExtension.MessageSender sender) {
                    if (!trusted(sender) || !(message instanceof JSONObject)) return GeckoResult.fromException(new SecurityException("untrusted_sender"));
                    JSONObject data = (JSONObject) message;
                    if ("migrateLegacyStorage".equals(data.optString("method"))) {
                        if (extensionBase == null || !sender.url.startsWith(extensionBase) || data.toString().length() > 512 * 1024) return GeckoResult.fromException(new SecurityException("untrusted_migration"));
                        localWeb.migrate(data.optJSONObject("options") == null ? new JSONObject() : data.optJSONObject("options"));
                        updateHandler.postDelayed(() -> { if (!isDestroyed()) loadLocalPage(); }, 50);
                        return GeckoResult.fromValue(new JSONObject());
                    }
                    GeckoResult<Object> result = new GeckoResult<>();
                    shell.invoke(data.optString("method"), data.optJSONObject("options") == null ? new JSONObject() : data.optJSONObject("options"), new NativeShellService.Reply() {
                        @Override public void success(JSONObject value) { runOnUiThread(() -> result.complete(value)); }
                        @Override public void failure(String code) { runOnUiThread(() -> result.completeExceptionally(new IllegalStateException(code))); }
                    });
                    return result;
                }
                @Override public void onConnect(WebExtension.Port port) {
                    if (!trusted(port.sender)) { port.disconnect(); return; }
                    pagePort = port;
                    updateHandler.removeCallbacksAndMessages(null);
                    updatePage.setVisibility(View.GONE);
                    localWeb.finishMigration();
                    getSharedPreferences("web-local-origin", 0).edit().putBoolean("migrated", true).apply();
                    updateIO.execute(() -> updater.confirm());
                    port.setDelegate(new WebExtension.PortDelegate() {
                        @Override public void onDisconnect(WebExtension.Port disconnected) { if (pagePort == disconnected) pagePort = null; }
                    });
                }
            };
            extension.setMessageDelegate(delegate, "photo_nini");
            session.getWebExtensionController().setMessageDelegate(extension, delegate, "photo_nini");
            if (!getSharedPreferences("web-local-origin", 0).getBoolean("migrated", false)) {
                session.loadUri(extensionBase + "migrate.html");
                updateHandler.postDelayed(() -> { if (pagePort == null) loadLocalPage(); }, 5000);
            } else loadLocalPage();
            updateHandler.postDelayed(() -> { if (pagePort == null && !rollbackUpdate()) showEngineFailure(); }, 45000);
        }, error -> showEngineFailure());
    }

    /** 仅清理本选择器一天前的随机缓存目录，避免图片长期滞留；不触碰其他缓存。 */
    private void clearExpiredPickedFiles() {
        File[] directories = getCacheDir().listFiles(file -> file.isDirectory() && file.getName().startsWith("picked-")
            && file.lastModified() < System.currentTimeMillis() - 86400000L);
        if (directories == null) return;
        for (File directory : directories) {
            File[] files = directory.listFiles();
            if (files != null) for (File file : files) if (file.isFile()) file.delete();
            directory.delete();
        }
    }

    /** 把本次获授权的供应商内容复制到随机私有目录；不接收网页指定的路径。 */
    private Uri copyPickedFile(Uri uri) throws Exception {
        String name = "selected-file";
        try (android.database.Cursor cursor = getContentResolver().query(uri,
                new String[] { OpenableColumns.DISPLAY_NAME }, null, null, null)) {
            if (cursor != null && cursor.moveToFirst()) name = cursor.getString(0);
        }
        if (name == null || name.isEmpty()) name = "selected-file";
        name = name.replaceAll("[\\\\/\\p{Cntrl}]", "_");
        if (".".equals(name) || "..".equals(name)) name = "selected-file";
        File directory = new File(getCacheDir(), "picked-" + java.util.UUID.randomUUID());
        if (!directory.mkdir()) throw new java.io.IOException("picker_cache_failed");
        File target = new File(directory, name);
        try (InputStream input = getContentResolver().openInputStream(uri);
             FileOutputStream output = new FileOutputStream(target)) {
            if (input == null) throw new java.io.IOException("picker_read_failed");
            byte[] buffer = new byte[64 * 1024];
            int length;
            while ((length = input.read(buffer)) != -1) output.write(buffer, 0, length);
        } catch (Exception error) { target.delete(); directory.delete(); throw error; }
        return Uri.fromFile(target);
    }

    /** 只信任本会话顶层的迁移扩展页或精确本机来源；不授权后台、子框架或其他扩展。 */
    private boolean trusted(WebExtension.MessageSender sender) {
        return EXTENSION_ID.equals(sender.webExtension.id) && sender.session == session && sender.isTopLevel()
            && ((extensionBase != null && sender.url.startsWith(extensionBase)) || (localWeb != null && localWeb.trusted(sender.url)));
    }
    /** 只向固定本机地址注入随机启动凭据，禁止网页控制请求头或目标。 */
    private void loadLocalPage() {
        session.load(new GeckoSession.Loader().uri(localWeb.startUrl())
            .additionalHeaders(localWeb.startHeaders()).headerFilter(GeckoSession.HEADER_FILTER_UNRESTRICTED_UNSAFE));
    }
    /** 候选启动失败时回到已确认资源；不会重新下载同一个失败版本。 */
    private boolean rollbackUpdate() {
        if (rollbackPending) return true;
        if (updater == null || !updater.hasCandidate()) return false;
        rollbackPending = true; pagePort = null;
        updateHandler.removeCallbacksAndMessages(null);
        updatePage.setVisibility(View.VISIBLE); status.setText(R.string.update_fallback);
        updateIO.execute(() -> {
            boolean recovered = updater.rollback();
            runOnUiThread(() -> {
                rollbackPending = false;
                if (isDestroyed()) return;
                if (!recovered) { showEngineFailure(); return; }
                try { if (session != null) session.close(); openEngine(); }
                catch (Exception error) { showEngineFailure(); }
            });
        });
        return true;
    }
    private void showEngineFailure() {
        if (isDestroyed()) return;
        updatePage.setVisibility(View.VISIBLE); status.setText(R.string.engine_failed); updateProgress.setIndeterminate(false); retryButton.setVisibility(View.VISIBLE);
    }
    private void openExternal(String value) {
        Uri uri = Uri.parse(value);
        if (!java.util.Arrays.asList("https", "http", "mailto", "tel").contains(uri.getScheme())) return;
        try { startActivity(new Intent(Intent.ACTION_VIEW, uri).addCategory(Intent.CATEGORY_BROWSABLE)); } catch (Exception ignored) { }
    }
    @Override protected void onStart() { super.onStart(); if (session != null) session.setActive(true); }
    @Override protected void onStop() { if (session != null) session.setActive(false); super.onStop(); }
    @Override protected void onDestroy() {
        if (fileResult != null && filePrompt != null) fileResult.complete(filePrompt.dismiss());
        if (session != null) session.close();
        if (shell != null) shell.close();
        updateHandler.removeCallbacksAndMessages(null);
        updateIO.shutdownNow();
        if (localWeb != null) localWeb.close();
        super.onDestroy();
    }
}
