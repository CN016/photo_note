package love.imcn016.photo;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.Intent;
import android.graphics.Color;
import android.net.Uri;
import android.util.Base64;
import androidx.activity.result.ActivityResult;
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
import java.nio.file.Files;

/** 系统文件导出与主题桥接：不接收任意本地路径，不申请共享存储权限，不记录用户内容。 */
public class NativeShellService {
    private final AppCompatActivity activity;
    private final ExecutorService io = Executors.newSingleThreadExecutor();
    private final ActivityResultLauncher<Intent> saveLauncher;
    private Call saveCall;

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
        saveLauncher = activity.registerForActivityResult(new ActivityResultContracts.StartActivityForResult(), result -> {
            Call call = saveCall;
            saveCall = null;
            saved(call, result);
        });
    }

    public void invoke(String method, JSONObject options, Reply reply) {
        Call call = new Call(options, reply);
        if ("exportFile".equals(method)) exportFile(call);
        else if ("setTheme".equals(method)) setTheme(call);
        else if ("minimizeApp".equals(method)) { activity.moveTaskToBack(true); call.resolve(); }
        else call.reject("unsupported_method");
    }

    public void close() { io.shutdown(); }

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
        if (remove) cleanup(); else { pendingFile = null; busy = false; }
        JSONObject result = new JSONObject();
        try { result.put("status", status); } catch (org.json.JSONException impossible) { call.reject("export_failed"); return; }
        call.resolve(result);
    }
}
