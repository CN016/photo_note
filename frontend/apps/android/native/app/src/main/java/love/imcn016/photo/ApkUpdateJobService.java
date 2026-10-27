package love.imcn016.photo;

import android.app.job.JobInfo;
import android.app.job.JobParameters;
import android.app.job.JobScheduler;
import android.app.job.JobService;
import android.content.ComponentName;
import android.content.Context;
import android.os.Handler;
import android.os.Looper;

/** 使用系统已有 JobScheduler 持久化每小时任务；省电和网络限制可延后执行，不申请精确闹钟。 */
public final class ApkUpdateJobService extends JobService {
    static final int JOB_ID = 0x504e01;
    static final long FLEX_MS = 5 * 60 * 1000L;
    private volatile JobParameters running;
    private Thread task;

    /** Activity 重建不能重置已有周期；系统在重启后恢复已登记任务。 */
    static void schedule(Context context) {
        JobScheduler scheduler = context.getSystemService(JobScheduler.class);
        if (scheduler == null || scheduler.getPendingJob(JOB_ID) != null) return;
        scheduler.schedule(new JobInfo.Builder(JOB_ID, new ComponentName(context, ApkUpdateJobService.class))
            .setPeriodic(ApkUpdateChecker.INTERVAL_MS, FLEX_MS)
            .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY).setPersisted(true).build());
    }
    @Override public boolean onStartJob(JobParameters params) {
        running = params;
        task = new Thread(() -> {
            try { new ApkUpdateChecker(getApplicationContext()).checkAt(System.currentTimeMillis(), params.getNetwork(), true); }
            catch (Exception ignored) { /* 配置不可用时不影响应用和原周期，下一次执行继续检查。 */ }
            new Handler(Looper.getMainLooper()).post(() -> {
                if (running == params) { running = null; jobFinished(params, false); }
            });
        }, "apk-update-job");
        task.start(); return true;
    }
    @Override public boolean onStopJob(JobParameters params) {
        running = null; if (task != null) task.interrupt(); return true;
    }
    @Override public void onDestroy() {
        running = null; if (task != null) task.interrupt(); super.onDestroy();
    }
}
