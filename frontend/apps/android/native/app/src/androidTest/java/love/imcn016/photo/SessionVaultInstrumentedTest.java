package love.imcn016.photo;
import static org.junit.Assert.*;
import android.content.Context;
import android.content.ContextWrapper;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.json.JSONObject;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.util.UUID;
/** 使用合成凭据验证原生加密、落盘、代次及退出边界，不访问真实用户会话。 */
@RunWith(AndroidJUnit4.class)
public class SessionVaultInstrumentedTest {
    private Context context() {
        Context app=InstrumentationRegistry.getInstrumentation().getTargetContext();
        File root=new File(app.getCacheDir(),"vault-fixture-"+UUID.randomUUID()); assertTrue(root.mkdir());
        return new ContextWrapper(app){ @Override public File getFilesDir(){return root;} };
    }
    private JSONObject pair(String refresh,long org,long version) throws Exception {return new JSONObject().put("refresh_token",refresh).put("session_id","fixture-session").put("orgid",org).put("session_version",version).put("access_token","never-persist-access");}
    @Test public void immediateRecreationRestoresEncryptedRefresh() throws Exception {
        Context context=context(); SessionVault vault=new SessionVault(context,"https://fixture.invalid/backendApi"); vault.accept(pair("fixture-refresh",17,2));
        String disk; try(InputStream input=new FileInputStream(new File(context.getFilesDir(),"session-vault-v1.json"))){disk=new String(BundleIO.read(input),StandardCharsets.UTF_8);}
        assertFalse(disk.contains("fixture-refresh")); assertFalse(disk.contains("never-persist-access"));
        String bootstrap=new SessionVault(context,"https://fixture.invalid/backendApi").bootstrap(); assertTrue(bootstrap.contains("fixture-refresh")); assertTrue(bootstrap.contains("session_version"));
        assertFalse(new SessionVault(context,"https://different.invalid").bootstrap().contains("fixture-refresh"));
    }
    @Test public void logoutAndLateResponsesCannotResurrectSession() throws Exception {
        Context context=context(); SessionVault vault=new SessionVault(context,"https://fixture.invalid"); vault.accept(pair("before-logout",-1,1)); long old=vault.generation();
        vault.logout(); vault.accept(pair("late-result",-1,1),old);
        vault.accept(pair("refresh-after-logout",-1,1),vault.generation());
        String bootstrap=new SessionVault(context,"https://fixture.invalid").bootstrap(); assertFalse(bootstrap.contains("late-result")); assertFalse(bootstrap.contains("refresh-after-logout")); assertTrue(bootstrap.contains("removeItem('photo-nini.session')"));
    }
    @Test public void staleSpaceOrRefreshRejectionCannotOverwriteNewCredentials() throws Exception {
        SessionVault vault=new SessionVault(context(),"https://fixture.invalid"); vault.accept(pair("new-space",23,3)); vault.accept(pair("old-space",17,2)); vault.reject("old-space");
        assertTrue(vault.bootstrap().contains("new-space")); assertFalse(vault.bootstrap().contains("old-space"));
        vault.reject("new-space"); assertFalse(vault.bootstrap().contains("new-space"));
    }
    @Test public void closedServerDiscardsLateSessionResponse() throws Exception {
        SessionVault vault=new SessionVault(context(),"https://fixture.invalid"); vault.accept(pair("stable",-1,1)); long epoch=vault.generation(); vault.stop(); vault.accept(pair("late",-1,1),epoch); assertTrue(vault.bootstrap().contains("stable")); assertFalse(vault.bootstrap().contains("late"));
    }
}
