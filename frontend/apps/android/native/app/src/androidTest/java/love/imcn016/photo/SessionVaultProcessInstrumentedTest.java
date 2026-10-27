package love.imcn016.photo;
import static org.junit.Assert.*;
import android.content.Context;
import android.content.ContextWrapper;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.json.JSONObject;
import java.io.File;
/** 使用独立的合成凭据目录，分两次仪器进程验证立即强制停止后仍能恢复，不等待浏览器落盘。 */
@RunWith(AndroidJUnit4.class)
public class SessionVaultProcessInstrumentedTest {
    @Test public void credentialsSurviveProcessRestart() throws Exception {
        Context app=InstrumentationRegistry.getInstrumentation().getTargetContext();
        File root=new File(app.getFilesDir(),"auth-persistence-fixture"); if(!root.exists()) assertTrue(root.mkdir());
        Context isolated=new ContextWrapper(app){@Override public File getFilesDir(){return root;}};
        SessionVault vault=new SessionVault(isolated,"https://fixture.invalid/process");
        boolean read="read".equals(InstrumentationRegistry.getArguments().getString("phase"));
        if(!read){vault.logout();vault.accept(new JSONObject().put("refresh_token","synthetic-process-refresh").put("session_id","synthetic-process-session").put("orgid",17).put("session_version",3));}
        assertTrue("全新进程从加密持久存储恢复凭据",new SessionVault(isolated,"https://fixture.invalid/process").bootstrap().contains("synthetic-process-refresh"));
    }
}
