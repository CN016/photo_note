package love.imcn016.photo;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ContentValues;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.media.MediaScannerConnection;
import android.provider.MediaStore;
import android.util.Base64;
import android.widget.Toast;
import androidx.activity.result.ActivityResult;
import androidx.core.content.ContextCompat;
import androidx.core.content.FileProvider;
import androidx.core.view.WindowCompat;
import org.json.JSONObject;
import androidx.appcompat.app.AppCompatActivity;
import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.io.IOException;
import java.nio.file.Files;

/**
 * 系统文件导出与主题桥接。
 *
 * 图片按 Android 版本写入系统相册的新条目，不再要求用户选择文件夹；其他文件继续
 * 使用 SAF 保存或只读 URI 分享。网页不能指定本地路径，本类也不记录用户内容或相册 URI。
 */
public class NativeShellService {
    private final AppCompatActivity activity;
    private final ExecutorService io = Executors.newSingleThreadExecutor();
    private final ActivityResultLauncher<Intent> saveLauncher;
    private Call saveCall;
    private final ActivityResultLauncher<String> albumPermission;
    private Call albumCall;
    private String albumMime = "";
    private boolean albumHandoff;
    private final ActivityResultLauncher<String> notificationPermission;
    private String monitorIdentity = "";
    private volatile String monitorOwner = "";
    private final java.util.concurrent.atomic.AtomicLong noticeGeneration = new java.util.concurrent.atomic.AtomicLong();
    private final ExecutorService noticeIO = Executors.newSingleThreadExecutor();
    private boolean permissionPending;
    private boolean notificationOpen;

    /** 所有响应只返回安全状态，不包含用户路径和文件内容。 */
    public interface Reply { void success(JSONObject result); void failure(String code); }
    public static class Call {
        private final JSONObject data;
        private final Reply reply;
        public Call(JSONObject data, Reply reply) { this.data = data; this.reply = reply; }
        public String getString(String key, String fallback) { return data.optString(key, fallback); }
        public String getString(String key) { return data.optString(key); }
        public boolean getBoolean(String key, boolean fallback) { return data.optBoolean(key, fallback); }
        public void resolve(JSONObject result) { reply.success(result); }
        public void resolve() { reply.success(new JSONObject()); }
        public void reject(String code) { reply.failure(code); }
    }

    /** 必须在 Activity STARTED 之前注册系统结果回调；实际文件读写使用专用串行线程。 */
    public NativeShellService(AppCompatActivity activity) {
        this.activity = activity;
        notificationOpen = NotificationMonitorService.OPEN_MESSAGES.equals(activity.getIntent().getAction());
        NotificationMonitorService.channels(activity);
        notificationPermission = activity.registerForActivityResult(new ActivityResultContracts.RequestPermission(), granted -> {
            permissionPending = false;
            if (granted) activity.getWindow().getDecorView().post(this::resumeNotifications);
        });
        saveLauncher = activity.registerForActivityResult(new ActivityResultContracts.StartActivityForResult(), result -> {
            Call call = saveCall;
            saveCall = null;
            saved(call, result);
        });
        albumPermission = activity.registerForActivityResult(new ActivityResultContracts.RequestPermission(), granted -> {
            Call call = albumCall;
            albumCall = null;
            String mime = albumMime;
            albumMime = "";
            boolean handoff = albumHandoff;
            albumHandoff = false;
            if (call == null && !handoff) {
                cleanup();
                return;
            }
            if (granted) {
                if (handoff) writeImageToAlbumHandoff(mime);
                else writeImageToAlbum(call, mime);
            }
            else {
                cleanup();
                if (handoff) showHandoffResult(false, true);
                else call.reject("album_permission_denied");
            }
        });
    }

    public void invoke(String method, JSONObject options, Reply reply) {
        Call call = new Call(options, reply);
        if ("exportFile".equals(method)) exportFile(call);
        else if ("exportImageToAlbum".equals(method)) exportImageToAlbum(call);
        else if ("setTheme".equals(method)) setTheme(call);
        else if ("minimizeApp".equals(method)) { activity.moveTaskToBack(true); call.resolve(); }
        else if ("independentNotificationSessions".equals(method)) call.resolve();
        else if ("startNotificationMonitor".equals(method)) {
            String owner=call.getString("identity"); monitorOwner=owner;
            long epoch=noticeGeneration.incrementAndGet();
            noticeIO.execute(() -> {
                try {
                    if(epoch!=noticeGeneration.get()) { call.resolve();return; }
                    String identity=noticeCredentials().install(owner,options.getJSONObject("tokens"));
                    activity.runOnUiThread(() -> {
                        if(epoch==noticeGeneration.get()) {monitorIdentity=identity; resumeNotifications();}
                        call.resolve();
                    });
                } catch(Exception unavailable) {call.reject("notification_session_failed");}
            });
        }
        else if ("stopNotificationMonitor".equals(method)) {
            String owner=call.getString("identity");
            // 停止只处理通知域；不退出浏览器，不改写Web的refresh token或退出墓碑。
            if(!monitorOwner.isEmpty() && !owner.equals(monitorOwner)) {call.resolve();return;}
            noticeGeneration.incrementAndGet();
            activity.runOnUiThread(() -> {
                if(owner.equals(monitorOwner) || monitorOwner.isEmpty()) {monitorIdentity="";monitorOwner="";activity.stopService(new Intent(activity,NotificationMonitorService.class));activity.getSystemService(android.app.NotificationManager.class).cancel(NotificationMonitorService.MESSAGE_ID);}
            });
            noticeIO.execute(() -> {try {noticeCredentials().clear(owner);call.resolve();}catch(Exception unavailable){call.reject("notification_session_failed");}});
        }
        else if ("consumeNotificationOpen".equals(method)) {
            activity.runOnUiThread(() -> { try { call.resolve(new JSONObject().put("open", notificationOpen)); notificationOpen = false; } catch (Exception ignored) { call.reject("notification_open_failed"); } });
        }
        else call.reject("unsupported_method");
    }

    /** 仅读取APK固定API配置；网页不得指定上游主机；只接收后端另行签发的通知凭据。 */
    private NotificationCredentials noticeCredentials() throws Exception {
        try (InputStream input = activity.getAssets().open("update-config.json")) {
            String api = new JSONObject(new String(SessionVault.readLimited(input), java.nio.charset.StandardCharsets.UTF_8)).getString("apiOrigin");
            return NotificationCredentials.get(activity, api);
        }
    }
    /** 默认启用，首次登录自动申请系统权限；拒绝后不反复弹窗，仍保留站内消息。 */
    void resumeNotifications() {
        if (monitorIdentity.isEmpty() || !activity.getLifecycle().getCurrentState().isAtLeast(androidx.lifecycle.Lifecycle.State.RESUMED)) return;
        if (android.os.Build.VERSION.SDK_INT >= 33 && androidx.core.content.ContextCompat.checkSelfPermission(activity, android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED) {
            var preferences = activity.getSharedPreferences("notification-permission", 0);
            if (!permissionPending && !preferences.getBoolean("requested", false)) {
                preferences.edit().putBoolean("requested", true).apply(); permissionPending = true;
                notificationPermission.launch(android.Manifest.permission.POST_NOTIFICATIONS);
            }
            return;
        }
        if (!activity.getSystemService(android.app.NotificationManager.class).areNotificationsEnabled()) return;
        try { activity.startForegroundService(new Intent(activity, NotificationMonitorService.class).putExtra("identity", monitorIdentity)); }
        catch (RuntimeException unavailable) { /* 系统拒绝启动或同步额度耗尽时保留站内消息，等下次用户打开应用。 */ }
    }
    void notificationIntent(Intent intent) { if (NotificationMonitorService.OPEN_MESSAGES.equals(intent.getAction())) notificationOpen = true; }

    public void close() { noticeGeneration.incrementAndGet(); noticeIO.shutdown(); io.shutdown(); }

    private static final int MAX_BYTES = 32 * 1024 * 1024;
    private File pendingFile;
    private boolean busy;

    /** 状态栏、导航栏跟随 Web 主题；不依赖用户设备的系统主题设置。 */
    public void setTheme(Call call) {
        boolean dark = Boolean.TRUE.equals(call.getBoolean("dark", false));
        activity.runOnUiThread(() -> {
            int color = Color.parseColor(dark ? "#101318" : "#F3F7FE");
            activity.getWindow().setStatusBarColor(color);
            activity.getWindow().setNavigationBarColor(color);
            activity.findViewById(android.R.id.content).setBackgroundColor(color);
            var controller = WindowCompat.getInsetsController(activity.getWindow(), activity.getWindow().getDecorView());
            controller.setAppearanceLightStatusBars(!dark);
            controller.setAppearanceLightNavigationBars(!dark);
            call.resolve();
        });
    }

    /** 临时文件只放内部缓存；最多一项导出，界面取消必须返回明确状态。 */
    public void exportFile(Call call) {
        synchronized (this) {
            if (busy) { call.reject("export_busy"); return; }
            busy = true;
        }
        String data = call.getString("data", "");
        String filename = call.getString("filename", "export");
        String mime = call.getString("mime", "application/octet-stream");
        if (filename.trim().isEmpty() || filename.length() > 180 || filename.contains("/") || filename.contains("\\") || filename.contains("\0") || data.length() > ((MAX_BYTES + 2L) / 3L) * 4L || !mime.matches("[a-zA-Z0-9.+-]+/[a-zA-Z0-9.+-]+")) {
            fail(call); return;
        }
        // 插件线程准备缓存；主线程仅显示系统面板，避免大文件写入卡住交互。
        io.execute(() -> {
            try {
                File directory = new File(activity.getCacheDir(), "exports");
                if (!directory.exists() && !directory.mkdirs()) throw new java.io.IOException();
                // 分享 URI 只授予只读权限；上一轮文件保留一天，避免接收应用延迟读取失败。
                File[] oldFiles = directory.listFiles();
                if (oldFiles != null) for (File file : oldFiles) {
                    if (file.lastModified() >= System.currentTimeMillis() - 86400000L) continue;
                    File[] contents = file.listFiles();
                    if (contents != null) for (File child : contents) child.delete();
                    file.delete();
                }
                File batch = new File(directory, java.util.UUID.randomUUID().toString());
                if (!batch.mkdir()) throw new java.io.IOException();
                pendingFile = new File(batch, filename);
                byte[] decoded = Base64.decode(data, Base64.NO_WRAP);
                if (decoded.length > MAX_BYTES) throw new java.io.IOException();
                try (OutputStream output = new FileOutputStream(pendingFile)) { output.write(decoded); }
                activity.runOnUiThread(() -> {
                    if (activity.isFinishing() || activity.isDestroyed()) { fail(call); return; }
                    if (mime.startsWith("image/")) {
                        saveImageToAlbum(call, mime);
                        return;
                    }
                    new AlertDialog.Builder(activity)
                    .setTitle(call.getString("title"))
                    .setItems(new String[] { call.getString("save"), call.getString("share") }, (dialog, which) -> {
                        if (which == 0) save(call, filename, mime); else share(call, mime);
                    })
                    .setNegativeButton(call.getString("cancel"), (dialog, which) -> complete(call, "cancelled", true))
                    .setOnCancelListener(dialog -> complete(call, "cancelled", true)).show();
                });
            } catch (Exception error) { fail(call); }
        });
    }

    /**
     * 图片导出交接模式。
     *
     * Web 发出任务后立即返回图片详情，不等待任何原生 Promise；真正的写入和
     * 相册结果由当前 Activity 异步完成并通过原生 Toast 通知。
     */
    public void exportImageToAlbum(Call call) {
        synchronized (this) {
            if (busy) {
                call.reject("export_busy");
                showHandoffResult(false, false);
                return;
            }
            busy = true;
        }
        String data = call.getString("data", "");
        String filename = call.getString("filename", "export");
        String mime = call.getString("mime", "application/octet-stream");
        if (!mime.startsWith("image/") || filename.trim().isEmpty() || filename.length() > 180
            || filename.contains("/") || filename.contains("\\") || filename.contains("\0")
            || data.length() > ((MAX_BYTES + 2L) / 3L) * 4L
            || !mime.matches("[a-zA-Z0-9.+-]+/[a-zA-Z0-9.+-]+")) {
            fail(call);
            showHandoffResult(false, false);
            return;
        }
        // 先确认 Activity 已接收；后续任何媒体处理结果都不再通过 Web 请求链路回传。
        try {
            call.resolve(new JSONObject().put("status", "accepted"));
        } catch (org.json.JSONException impossible) {
            fail(call);
            return;
        }
        io.execute(() -> {
            try {
                File directory = new File(activity.getCacheDir(), "exports");
                if (!directory.exists() && !directory.mkdirs()) throw new java.io.IOException();
                File batch = new File(directory, java.util.UUID.randomUUID().toString());
                if (!batch.mkdir()) throw new java.io.IOException();
                pendingFile = new File(batch, filename);
                byte[] decoded = Base64.decode(data, Base64.NO_WRAP);
                if (decoded.length > MAX_BYTES) throw new java.io.IOException();
                try (OutputStream output = new FileOutputStream(pendingFile)) { output.write(decoded); }
                activity.runOnUiThread(() -> {
                    if (activity.isFinishing() || activity.isDestroyed()) {
                        cleanup();
                        return;
                    }
                    saveImageToAlbumHandoff(call, mime);
                });
            } catch (Exception error) {
                cleanup();
                showHandoffResult(false, false);
            }
        });
    }

    /**
     * 图片直接进入系统相册。
     *
     * API 29 起通过 MediaStore 分区存储写入，无需申请存储权限；Android 9 仍需
     * WRITE_EXTERNAL_STORAGE 才能写入公共 Pictures 目录，因此只在该系统版本请求。
     */
    private void saveImageToAlbum(Call call, String mime) {
        albumHandoff = false;
        if (Build.VERSION.SDK_INT <= Build.VERSION_CODES.P
            && ContextCompat.checkSelfPermission(activity, android.Manifest.permission.WRITE_EXTERNAL_STORAGE) != PackageManager.PERMISSION_GRANTED) {
            albumCall = call;
            albumMime = mime;
            albumPermission.launch(android.Manifest.permission.WRITE_EXTERNAL_STORAGE);
            return;
        }
        writeImageToAlbum(call, mime);
    }

    /** 交接模式需要 Android 9 写入权限；授权结果只触发原生 Toast，不再回写 Web。 */
    private void saveImageToAlbumHandoff(Call call, String mime) {
        if (Build.VERSION.SDK_INT <= Build.VERSION_CODES.P
            && ContextCompat.checkSelfPermission(activity, android.Manifest.permission.WRITE_EXTERNAL_STORAGE) != PackageManager.PERMISSION_GRANTED) {
            // 交接请求已经向 Web 返回，不保留包含 Base64 的 Call，避免权限弹窗期间占用大对象。
            albumCall = null;
            albumMime = mime;
            albumHandoff = true;
            albumPermission.launch(android.Manifest.permission.WRITE_EXTERNAL_STORAGE);
            return;
        }
        writeImageToAlbumHandoff(mime);
    }

    /** 交接模式只清理缓存和显示原生结果，不调用已结束的 Web Call。 */
    private void writeImageToAlbumHandoff(String mime) {
        io.execute(() -> {
            boolean saved = false;
            try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) saveImageThroughMediaStore(mime);
                else saveImageToLegacyPictures(mime);
                saved = true;
            } catch (Exception error) {
                saved = false;
            } finally {
                cleanup();
            }
            showHandoffResult(saved, false);
        });
    }

    /** Activity 自有提示不依赖 Web toast 或长连接生命周期。 */
    private void showHandoffResult(boolean saved, boolean permissionDenied) {
        if (activity.isFinishing() || activity.isDestroyed()) return;
        int text = saved ? R.string.export_image_saved
            : permissionDenied ? R.string.export_image_permission_denied
            : R.string.export_image_failed;
        activity.runOnUiThread(() -> Toast.makeText(activity, text, Toast.LENGTH_SHORT).show());
    }

    /** 在导出线程复制缓存文件；只有相册条目或公共文件完整写入后才返回 saved。 */
    private void writeImageToAlbum(Call call, String mime) {
        io.execute(() -> {
            try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) saveImageThroughMediaStore(mime);
                else saveImageToLegacyPictures(mime);
                complete(call, "saved", true);
            } catch (Exception error) {
                fail(call);
            }
        });
    }

    /** 新系统以 IS_PENDING 隐藏未完成文件，避免相册短暂显示半张照片。 */
    private void saveImageThroughMediaStore(String mime) throws IOException {
        ContentValues values = new ContentValues();
        values.put(MediaStore.Images.Media.DISPLAY_NAME, uniqueMediaDisplayName(pendingFile.getName()));
        values.put(MediaStore.Images.Media.MIME_TYPE, mime);
        values.put(MediaStore.Images.Media.RELATIVE_PATH, Environment.DIRECTORY_PICTURES + File.separator + "LightNote");
        values.put(MediaStore.Images.Media.IS_PENDING, 1);
        Uri uri = activity.getContentResolver().insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values);
        if (uri == null) throw new IOException("media_insert_failed");
        try {
            try (InputStream input = Files.newInputStream(pendingFile.toPath());
                 OutputStream output = activity.getContentResolver().openOutputStream(uri, "w")) {
                if (output == null) throw new IOException("media_output_unavailable");
                copy(input, output);
            }
            ContentValues ready = new ContentValues();
            ready.put(MediaStore.Images.Media.IS_PENDING, 0);
            // 某些 MediaStore 实现写入成功后仍返回 0；文件已完整关闭即按成功处理，避免误报失败。
            activity.getContentResolver().update(uri, ready, null, null);
        } catch (Exception error) {
            activity.getContentResolver().delete(uri, null, null);
            throw error;
        }
    }

    /** Android 9 写入标准 Pictures/LightNote，并交给媒体扫描器登记到相册。 */
    private void saveImageToLegacyPictures(String mime) throws IOException {
        File pictures = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_PICTURES);
        File album = new File(pictures, "LightNote");
        if (!album.exists() && !album.mkdirs()) throw new IOException("album_directory_unavailable");
        File destination = uniqueLegacyFile(album, pendingFile.getName());
        try (InputStream input = Files.newInputStream(pendingFile.toPath());
             OutputStream output = new FileOutputStream(destination)) {
            copy(input, output);
        } catch (Exception error) {
            destination.delete();
            throw error;
        }
        // 公共 Pictures 文件已完整写入即可视为保存成功；媒体扫描回调部分厂商会迟到或返回空 URI，
        // 不能据此撤销已落盘照片或向网页误报失败。扫描器通常会在下一次媒体刷新时补登记。
        MediaScannerConnection.scanFile(activity, new String[]{ destination.getAbsolutePath() }, new String[]{ mime }, null);
    }

    /** 同名导出追加序号，确保每次导出都是相册中的新照片，不覆盖历史文件。 */
    private String uniqueMediaDisplayName(String filename) {
        String base = filename;
        String extension = "";
        int dot = filename.lastIndexOf('.');
        if (dot > 0) {
            base = filename.substring(0, dot);
            extension = filename.substring(dot);
        }
        for (int index = 0; index < 100; index += 1) {
            String candidate = index == 0 ? filename : base + " (" + index + ")" + extension;
            try (Cursor cursor = activity.getContentResolver().query(
                MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
                new String[]{ MediaStore.Images.Media._ID },
                MediaStore.Images.Media.DISPLAY_NAME + "=?",
                new String[]{ candidate },
                null
            )) {
                if (cursor == null || !cursor.moveToFirst()) return candidate;
            }
        }
        return base + "-" + System.currentTimeMillis() + extension;
    }

    /** 旧系统文件系统没有 MediaStore 的唯一命名保证，逐个检查同目录文件。 */
    private File uniqueLegacyFile(File directory, String filename) {
        String base = filename;
        String extension = "";
        int dot = filename.lastIndexOf('.');
        if (dot > 0) {
            base = filename.substring(0, dot);
            extension = filename.substring(dot);
        }
        for (int index = 0; index < 100; index += 1) {
            String candidate = index == 0 ? filename : base + " (" + index + ")" + extension;
            File file = new File(directory, candidate);
            if (!file.exists()) return file;
        }
        return new File(directory, base + "-" + System.currentTimeMillis() + extension);
    }

    /** 使用 64 KiB 缓冲复制，避免把导出图片再次完整解码到 Java 内存。 */
    private void copy(InputStream input, OutputStream output) throws IOException {
        byte[] buffer = new byte[65536];
        int count;
        while ((count = input.read(buffer)) != -1) output.write(buffer, 0, count);
    }

    /** SAF 创建文件由用户选择目标；绝不根据网页参数直接覆盖本地路径。 */
    private void save(Call call, String filename, String mime) {
        try {
            Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType(mime).putExtra(Intent.EXTRA_TITLE, filename);
            saveCall = call;
            saveLauncher.launch(intent);
        } catch (Exception error) { fail(call); }
    }

    /** 系统选择器取消不报错；写入完成后才返回 saved，失败不泄漏 URI 或文件内容。 */
    private void saved(Call call, ActivityResult result) {
        if (call == null) { cleanup(); return; }
        if (result.getResultCode() != Activity.RESULT_OK || result.getData() == null || result.getData().getData() == null) { complete(call, "cancelled", true); return; }
        Uri uri = result.getData().getData();
        io.execute(() -> {
            try (InputStream input = Files.newInputStream(pendingFile.toPath()); OutputStream output = activity.getContentResolver().openOutputStream(uri, "w")) {
                if (output == null) throw new java.io.IOException();
                byte[] buffer = new byte[65536];
                int count;
                while ((count = input.read(buffer)) != -1) output.write(buffer, 0, count);
            } catch (Exception error) { fail(call); return; }
            complete(call, "saved", true);
        });
    }

    /** 分享只授权指定缓存文件；shared 表示系统面板已打开，不声称接收方已送达。 */
    private void share(Call call, String mime) {
        try {
            Uri uri = FileProvider.getUriForFile(activity, activity.getPackageName() + ".fileprovider", pendingFile);
            Intent send = new Intent(Intent.ACTION_SEND).setType(mime).putExtra(Intent.EXTRA_STREAM, uri)
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
            send.setClipData(ClipData.newRawUri("export", uri));
            activity.startActivity(Intent.createChooser(send, call.getString("share")));
            complete(call, "shared", false);
        } catch (Exception error) { fail(call); }
    }

    /** 删除未分享缓存并释放串行状态；即使删除失败也不把用户文件路径暴露给网页。 */
    private synchronized void cleanup() {
        if (pendingFile != null) { pendingFile.delete(); pendingFile.getParentFile().delete(); }
        pendingFile = null;
        busy = false;
    }

    private void fail(Call call) { cleanup(); call.reject("export_failed"); }

    private synchronized void complete(Call call, String status, boolean remove) {
        JSONObject result = new JSONObject();
        try { result.put("status", status); } catch (org.json.JSONException impossible) { call.reject("export_failed"); return; }
        // 先返回成功，再清理临时缓存；清理属于保存后的收尾，不能让已成功照片变成失败提示。
        call.resolve(result);
        if (remove) cleanup(); else { pendingFile = null; busy = false; }
    }
}
