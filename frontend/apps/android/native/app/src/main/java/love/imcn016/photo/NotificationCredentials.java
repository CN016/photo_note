package love.imcn016.photo;

import android.content.Context;
import org.json.JSONObject;

/** 通知专属凭据域：与浏览器凭据不共享文件、密钥、实例或网络锁。只由通知线程更新。 */
final class NotificationCredentials {
    private static NotificationCredentials instance;
    final SessionVault vault;
    private final android.content.SharedPreferences state;
    private final String origin;
    private NotificationCredentials(Context context,String origin) {
        this.origin=origin; vault=new SessionVault(context,origin,true);
        state=context.getSharedPreferences("notice-owner-v1",0);
    }
    static synchronized NotificationCredentials get(Context context,String origin) {
        if(instance==null || !instance.origin.equals(origin)) instance=new NotificationCredentials(context.getApplicationContext(),origin);
        return instance;
    }
    String owner() { return state.getString("owner",""); }
    synchronized String install(String owner,JSONObject tokens) throws Exception {
        String[] parts=owner.split(":");
        if(parts.length!=3 || !Long.toString(tokens.getLong("orgid")).equals(parts[1]) || tokens.getString("session_id").equals(parts[0])) throw new SecurityException("invalid_notice_session");
        vault.accept(tokens);
        if(!state.edit().putString("owner",owner).commit()) throw new java.io.IOException("notice_owner");
        return vault.identity();
    }
    synchronized void clear(String owner) throws Exception {
        if(!owner.equals(owner())) return;
        vault.logout();state.edit().remove("owner").commit();
    }
}
